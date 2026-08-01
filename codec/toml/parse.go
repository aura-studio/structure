// Package toml parses and encodes TOML text as a node.Node tree.
//
// BurntSushi/toml decodes into an unordered Go map, so the parser replays
// MetaData.Keys() — every key path in document order — onto OrderedMap trees to
// recover key order. The four TOML temporal types are normalized to strings
// rather than time.Time.
package toml

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	bstoml "github.com/BurntSushi/toml"

	"github.com/aura-studio/structure/v2/format"
	"github.com/aura-studio/structure/v2/node"
)

// Sentinel *time.Location names BurntSushi/toml uses for the three local TOML
// temporal types; offset date-times carry a real location instead.
const (
	locDate     = "date-local"
	locDateTime = "datetime-local"
	locTime     = "time-local"
)

// Parse decodes a TOML document into a node.Node tree.
func Parse(input string) (node.Node, error) {
	var raw map[string]any
	md, err := bstoml.Decode(input, &raw)
	if err != nil {
		return nil, wrapErr(err)
	}
	r := &replay{raw: raw, root: node.NewOrderedMap(), cursor: map[string]int{}}
	for _, key := range md.Keys() {
		if len(key) > node.MaxDepth {
			return nil, node.ErrTooDeep
		}
		if err := r.apply(key); err != nil {
			return nil, err
		}
	}
	return r.root, nil
}

// replay rebuilds document order by walking the decoded map and the OrderedMap
// tree in lockstep. Arrays of tables need a cursor: [[a]] appears once per
// occurrence in Keys(), and the leaf paths that follow it belong to the element
// that occurrence opened. Cursor keys embed the indices of every enclosing array
// element, so a nested [[a.b]] restarts at 0 inside each new element of a.
type replay struct {
	raw    map[string]any
	root   *node.OrderedMap
	cursor map[string]int
}

// cursorSeg and cursorIdx encode one component of a cursor key. Every component
// is length-prefixed (or colon-terminated) so the encoding is injective. A naive
// "\x00"+seg / "#"+idx concatenation is not: element 0 of a real [[a]] array and
// an ordinary quoted key "a#0" both produce "\x00a#0", so the two unrelated
// nodes share one counter. The counter is then advanced by the wrong
// occurrences, which silently drops leaf values (resolve skips an index past the
// real element count) and grows phantom empty tables.
func cursorSeg(s string) string { return "s" + strconv.Itoa(len(s)) + ":" + s }

func cursorIdx(i int) string { return "i" + strconv.Itoa(i) + ":" }

// apply replays one key path from MetaData.Keys().
func (r *replay) apply(key []string) error {
	val, owner, instance, found, err := r.resolve(key)
	if err != nil || !found {
		return err
	}
	leaf := key[len(key)-1]
	switch val.(type) {
	case map[string]any:
		// Table header or inline table: a placeholder is created here and its
		// leaves arrive as their own key paths.
		existing, ok := owner.Get(leaf)
		if !ok {
			owner.Set(leaf, node.NewOrderedMap())
			return nil
		}
		if _, ok := existing.(*node.OrderedMap); !ok {
			return fmt.Errorf("toml: conflicting key %q (not a table)", leaf)
		}
		return nil
	case []map[string]any:
		// Header array of tables: [[a]] shows up once per occurrence, so the
		// cursor advances on every sighting after the first.
		path := instance + cursorSeg(leaf)
		if _, seen := r.cursor[path]; seen {
			r.cursor[path]++
		} else {
			r.cursor[path] = 0
		}
		return r.ensureElement(owner, leaf, r.cursor[path])
	default:
		nv, err := anyToNode(val, len(key)+1)
		if err != nil {
			return err
		}
		owner.Set(leaf, nv)
		return nil
	}
}

// resolve walks all but the last segment of key against both the decoded map
// and the tree, following the current element of every array of tables. It
// returns the raw value at the final segment, the tree table that owns it, and
// the instance path of that owner.
func (r *replay) resolve(key []string) (any, *node.OrderedMap, string, bool, error) {
	// Walk the decoded data first. Any path that does not resolve there cannot
	// be replayed and is skipped: inline arrays of tables decode to []any, and
	// BurntSushi still reports their member paths (`x = [{a = 1}]` yields both
	// "x" and "x.a"), even though "x" was already converted wholesale.
	type step struct {
		seg string
		idx int // element index for arrays of tables, -1 otherwise
	}
	var steps []step
	var rawCur any = r.raw
	instance := ""
	for _, seg := range key[:len(key)-1] {
		m, ok := rawCur.(map[string]any)
		if !ok {
			return nil, nil, "", false, nil
		}
		next, ok := m[seg]
		if !ok {
			return nil, nil, "", false, nil
		}
		instance += cursorSeg(seg)
		elems, isArrayOfTables := next.([]map[string]any)
		if !isArrayOfTables {
			rawCur = next
			steps = append(steps, step{seg: seg, idx: -1})
			continue
		}
		idx := r.cursor[instance]
		if idx >= len(elems) {
			return nil, nil, "", false, nil
		}
		rawCur = elems[idx]
		instance += cursorIdx(idx)
		steps = append(steps, step{seg: seg, idx: idx})
	}
	m, ok := rawCur.(map[string]any)
	if !ok {
		return nil, nil, "", false, nil
	}
	val, ok := m[key[len(key)-1]]
	if !ok {
		return nil, nil, "", false, nil
	}

	// The raw path resolved, so mirror it onto the tree.
	tree := r.root
	for _, s := range steps {
		cur, exists := tree.Get(s.seg)
		if s.idx < 0 {
			if !exists {
				sub := node.NewOrderedMap()
				tree.Set(s.seg, sub)
				tree = sub
				continue
			}
			sub, ok := cur.(*node.OrderedMap)
			if !ok {
				return nil, nil, "", false, fmt.Errorf("toml: conflicting key %q (not a table)", s.seg)
			}
			tree = sub
			continue
		}
		if !exists {
			return nil, nil, "", false, nil
		}
		arr, ok := cur.([]node.Node)
		if !ok || s.idx >= len(arr) {
			return nil, nil, "", false, fmt.Errorf("toml: conflicting key %q (not an array of tables)", s.seg)
		}
		elem, ok := arr[s.idx].(*node.OrderedMap)
		if !ok {
			return nil, nil, "", false, fmt.Errorf("toml: conflicting key %q (not a table)", s.seg)
		}
		tree = elem
	}
	return val, tree, instance, true, nil
}

// ensureElement grows the array of tables at leaf so index idx exists.
func (r *replay) ensureElement(owner *node.OrderedMap, leaf string, idx int) error {
	existing, ok := owner.Get(leaf)
	if !ok {
		arr := make([]node.Node, 0, idx+1)
		for i := 0; i <= idx; i++ {
			arr = append(arr, node.NewOrderedMap())
		}
		owner.Set(leaf, arr)
		return nil
	}
	arr, ok := existing.([]node.Node)
	if !ok {
		return fmt.Errorf("toml: conflicting key %q (not an array of tables)", leaf)
	}
	for len(arr) <= idx {
		arr = append(arr, node.NewOrderedMap())
	}
	owner.Set(leaf, arr)
	return nil
}

// anyToNode converts a decoded Go value into the node.Node model.
func anyToNode(v any, depth int) (node.Node, error) {
	if err := node.TooDeep(depth); err != nil {
		return nil, err
	}
	switch tv := v.(type) {
	case nil:
		return nil, nil
	case bool:
		return tv, nil
	case string:
		return tv, nil
	case int64:
		return tv, nil
	case float64:
		return tv, nil
	case time.Time:
		return timeString(tv), nil
	case map[string]any:
		// Reached only for inline tables, whose key order Go's map does not
		// preserve; sort for deterministic output.
		m := node.NewOrderedMap()
		keys := make([]string, 0, len(tv))
		for k := range tv {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			nv, err := anyToNode(tv[k], depth+1)
			if err != nil {
				return nil, err
			}
			m.Set(k, nv)
		}
		return m, nil
	case []map[string]any:
		arr := make([]node.Node, len(tv))
		for i, e := range tv {
			nv, err := anyToNode(e, depth+1)
			if err != nil {
				return nil, err
			}
			arr[i] = nv
		}
		return arr, nil
	case []any:
		arr := make([]node.Node, len(tv))
		for i, e := range tv {
			nv, err := anyToNode(e, depth+1)
			if err != nil {
				return nil, err
			}
			arr[i] = nv
		}
		return arr, nil
	}
	return nil, fmt.Errorf("toml: unsupported decoded type %T", v)
}

// timeString normalizes the four TOML temporal types to strings.
func timeString(t time.Time) string {
	switch t.Location().String() {
	case locDate:
		return t.Format("2006-01-02")
	case locDateTime:
		return t.Format("2006-01-02T15:04:05.999999999")
	case locTime:
		return t.Format("15:04:05.999999999")
	default:
		return t.Format(time.RFC3339Nano)
	}
}

func wrapErr(err error) error {
	if pe, ok := err.(bstoml.ParseError); ok {
		return format.Wrap(format.TOML, int(pe.Position.Line), int(pe.Position.Col), err, pe.Message)
	}
	return format.Wrap(format.TOML, 0, 0, err, err.Error())
}
