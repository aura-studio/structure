package structure

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Sentinel *time.Location names BurntSushi/toml uses for the three local TOML
// temporal types; offset date-times carry a real location instead.
const (
	tomlLocDate     = "date-local"
	tomlLocDateTime = "datetime-local"
	tomlLocTime     = "time-local"
)

// parseTOML decodes a TOML document. BurntSushi yields an unordered Go map;
// MetaData.Keys() returns every key path in document order, which we replay
// onto OrderedMap trees to recover key order.
func parseTOML(input string) (Node, error) {
	var raw map[string]any
	md, err := toml.Decode(input, &raw)
	if err != nil {
		return nil, wrapTOMLErr(err)
	}
	r := &tomlReplay{raw: raw, root: NewOrderedMap(), cursor: map[string]int{}}
	for _, key := range md.Keys() {
		if len(key) > maxDepth {
			return nil, ErrTooDeep
		}
		if err := r.apply(key); err != nil {
			return nil, err
		}
	}
	return r.root, nil
}

// tomlReplay rebuilds document order by walking the decoded map and the
// OrderedMap tree in lockstep. Arrays of tables need a cursor: [[a]] appears
// once per occurrence in Keys(), and the leaf paths that follow it belong to
// the element that occurrence opened. Cursor keys embed the indices of every
// enclosing array element, so a nested [[a.b]] restarts at 0 inside each new
// element of a.
type tomlReplay struct {
	raw    map[string]any
	root   *OrderedMap
	cursor map[string]int
}

// apply replays one key path from MetaData.Keys().
func (r *tomlReplay) apply(key []string) error {
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
			owner.Set(leaf, NewOrderedMap())
			return nil
		}
		if _, ok := existing.(*OrderedMap); !ok {
			return fmt.Errorf("toml: conflicting key %q (not a table)", leaf)
		}
		return nil
	case []map[string]any:
		// Header array of tables: [[a]] shows up once per occurrence, so the
		// cursor advances on every sighting after the first.
		path := instance + "\x00" + leaf
		if _, seen := r.cursor[path]; seen {
			r.cursor[path]++
		} else {
			r.cursor[path] = 0
		}
		return r.ensureElement(owner, leaf, r.cursor[path])
	default:
		node, err := tomlAnyToNode(val, len(key)+1)
		if err != nil {
			return err
		}
		owner.Set(leaf, node)
		return nil
	}
}

// resolve walks all but the last segment of key against both the decoded map
// and the tree, following the current element of every array of tables. It
// returns the raw value at the final segment, the tree table that owns it, and
// the instance path of that owner.
func (r *tomlReplay) resolve(key []string) (any, *OrderedMap, string, bool, error) {
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
		instance += "\x00" + seg
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
		instance += "#" + strconv.Itoa(idx)
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
		node, exists := tree.Get(s.seg)
		if s.idx < 0 {
			if !exists {
				sub := NewOrderedMap()
				tree.Set(s.seg, sub)
				tree = sub
				continue
			}
			sub, ok := node.(*OrderedMap)
			if !ok {
				return nil, nil, "", false, fmt.Errorf("toml: conflicting key %q (not a table)", s.seg)
			}
			tree = sub
			continue
		}
		if !exists {
			return nil, nil, "", false, nil
		}
		arr, ok := node.([]Node)
		if !ok || s.idx >= len(arr) {
			return nil, nil, "", false, fmt.Errorf("toml: conflicting key %q (not an array of tables)", s.seg)
		}
		elem, ok := arr[s.idx].(*OrderedMap)
		if !ok {
			return nil, nil, "", false, fmt.Errorf("toml: conflicting key %q (not a table)", s.seg)
		}
		tree = elem
	}
	return val, tree, instance, true, nil
}

// ensureElement grows the array of tables at leaf so index idx exists.
func (r *tomlReplay) ensureElement(owner *OrderedMap, leaf string, idx int) error {
	existing, ok := owner.Get(leaf)
	if !ok {
		arr := make([]Node, 0, idx+1)
		for i := 0; i <= idx; i++ {
			arr = append(arr, NewOrderedMap())
		}
		owner.Set(leaf, arr)
		return nil
	}
	arr, ok := existing.([]Node)
	if !ok {
		return fmt.Errorf("toml: conflicting key %q (not an array of tables)", leaf)
	}
	for len(arr) <= idx {
		arr = append(arr, NewOrderedMap())
	}
	owner.Set(leaf, arr)
	return nil
}

// tomlAnyToNode converts a decoded Go value into the Node model.
func tomlAnyToNode(v any, depth int) (Node, error) {
	if err := tooDeep(depth); err != nil {
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
		return tomlTimeString(tv), nil
	case map[string]any:
		// Reached only for inline tables, whose key order Go's map does not
		// preserve; sort for deterministic output.
		m := NewOrderedMap()
		keys := make([]string, 0, len(tv))
		for k := range tv {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			nv, err := tomlAnyToNode(tv[k], depth+1)
			if err != nil {
				return nil, err
			}
			m.Set(k, nv)
		}
		return m, nil
	case []map[string]any:
		arr := make([]Node, len(tv))
		for i, e := range tv {
			nv, err := tomlAnyToNode(e, depth+1)
			if err != nil {
				return nil, err
			}
			arr[i] = nv
		}
		return arr, nil
	case []any:
		arr := make([]Node, len(tv))
		for i, e := range tv {
			nv, err := tomlAnyToNode(e, depth+1)
			if err != nil {
				return nil, err
			}
			arr[i] = nv
		}
		return arr, nil
	}
	return nil, fmt.Errorf("toml: unsupported decoded type %T", v)
}

// tomlTimeString normalizes the four TOML temporal types to strings
// (Requirement 7.4).
func tomlTimeString(t time.Time) string {
	switch t.Location().String() {
	case tomlLocDate:
		return t.Format("2006-01-02")
	case tomlLocDateTime:
		return t.Format("2006-01-02T15:04:05.999999999")
	case tomlLocTime:
		return t.Format("15:04:05.999999999")
	default:
		return t.Format(time.RFC3339Nano)
	}
}

func wrapTOMLErr(err error) error {
	if pe, ok := err.(toml.ParseError); ok {
		return &ParseError{Format: TOML, Line: int(pe.Position.Line), Column: int(pe.Position.Col), Msg: pe.Message, err: err}
	}
	return &ParseError{Format: TOML, Msg: err.Error(), err: err}
}

// encodeTOML renders a Node tree as TOML text with 2-space indentation.
func encodeTOML(n Node) (string, error) {
	if err := validate(n); err != nil {
		return "", err
	}
	m, ok := n.(*OrderedMap)
	if !ok {
		return "", fmt.Errorf("%w: TOML requires a table at the root (got array)", ErrUnsupportedStructure)
	}
	var b strings.Builder
	if err := writeTOMLTableBody(&b, m, nil, 0); err != nil {
		return "", err
	}
	out := b.String()
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out, nil
}

// isArrayOfTables reports whether arr is a non-empty array whose elements are
// all mappings — encodable as [[...]] arrays of tables.
func isArrayOfTables(arr []Node) bool {
	if len(arr) == 0 {
		return false
	}
	for _, e := range arr {
		if _, ok := e.(*OrderedMap); !ok {
			return false
		}
	}
	return true
}

// writeTOMLTableBody emits the keyvals of m, then its subtables and arrays of
// tables. path is the section path of m (nil for root); level is the indent.
func writeTOMLTableBody(b *strings.Builder, m *OrderedMap, path []string, level int) error {
	// Pass 1: simple values (scalars, inline arrays, inline tables).
	for k, v := range m.All() {
		switch tv := v.(type) {
		case *OrderedMap:
			continue // subtable, pass 2
		case []Node:
			if isArrayOfTables(tv) {
				continue // array of tables, pass 3
			}
		}
		writeTOMLIndent(b, level)
		b.WriteString(tomlKey(k))
		b.WriteString(" = ")
		if err := writeTOMLValue(b, v); err != nil {
			return err
		}
		b.WriteByte('\n')
	}

	// Pass 2: subtables (headers are always emitted, even for empty tables,
	// so round-trips preserve them).
	for k, v := range m.All() {
		sub, ok := v.(*OrderedMap)
		if !ok {
			continue
		}
		full := append(append([]string(nil), path...), k)
		tomlSectionBreak(b)
		b.WriteString("[")
		b.WriteString(tomlKeyPath(full))
		b.WriteString("]\n")
		if err := writeTOMLTableBody(b, sub, full, level+1); err != nil {
			return err
		}
	}

	// Pass 3: arrays of tables.
	for k, v := range m.All() {
		arr, ok := v.([]Node)
		if !ok || !isArrayOfTables(arr) {
			continue
		}
		full := append(append([]string(nil), path...), k)
		for _, e := range arr {
			tomlSectionBreak(b)
			b.WriteString("[[")
			b.WriteString(tomlKeyPath(full))
			b.WriteString("]]\n")
			if err := writeTOMLTableBody(b, e.(*OrderedMap), full, level+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// tomlSectionBreak inserts a blank line before a new section unless the
// buffer is empty or already ends with a blank line.
func tomlSectionBreak(b *strings.Builder) {
	s := b.String()
	if s != "" && !strings.HasSuffix(s, "\n\n") {
		b.WriteByte('\n')
	}
}

func writeTOMLIndent(b *strings.Builder, level int) {
	for i := 0; i < level; i++ {
		b.WriteString("  ")
	}
}

// writeTOMLValue renders any value in inline position.
func writeTOMLValue(b *strings.Builder, n Node) error {
	switch v := n.(type) {
	case nil:
		return fmt.Errorf("%w: TOML has no null value", ErrUnsupportedStructure)
	case bool:
		b.WriteString(strconv.FormatBool(v))
	case string:
		b.WriteString(tomlBasicString(v))
	case int64:
		b.WriteString(strconv.FormatInt(v, 10))
	case uint64:
		if v > math.MaxInt64 {
			return fmt.Errorf("%w: TOML integers are limited to int64 (got %d)", ErrUnsupportedStructure, v)
		}
		b.WriteString(strconv.FormatUint(v, 10))
	case *big.Int:
		if v == nil {
			return fmt.Errorf("structure: invalid node: nil *big.Int")
		}
		if !v.IsInt64() {
			return fmt.Errorf("%w: TOML integers are limited to int64 (got %s)", ErrUnsupportedStructure, v.String())
		}
		b.WriteString(v.String())
	case float64:
		switch {
		case math.IsNaN(v):
			b.WriteString("nan")
		case math.IsInf(v, 1):
			b.WriteString("inf")
		case math.IsInf(v, -1):
			b.WriteString("-inf")
		default:
			s, err := formatFloat(v)
			if err != nil {
				return err
			}
			b.WriteString(s)
		}
	case *OrderedMap:
		return writeTOMLInlineTable(b, v)
	case []Node:
		return writeTOMLInlineArray(b, v)
	default:
		return fmt.Errorf("structure: invalid node type %T", n)
	}
	return nil
}

func writeTOMLInlineTable(b *strings.Builder, m *OrderedMap) error {
	b.WriteString("{")
	first := true
	for k, v := range m.All() {
		if !first {
			b.WriteString(", ")
		}
		first = false
		b.WriteString(tomlKey(k))
		b.WriteString(" = ")
		if err := writeTOMLValue(b, v); err != nil {
			return err
		}
	}
	b.WriteString("}")
	return nil
}

func writeTOMLInlineArray(b *strings.Builder, arr []Node) error {
	b.WriteString("[")
	for i, e := range arr {
		if i > 0 {
			b.WriteString(", ")
		}
		if err := writeTOMLValue(b, e); err != nil {
			return err
		}
	}
	b.WriteString("]")
	return nil
}

// tomlKey renders a bare key when possible, else a quoted key.
func tomlKey(k string) string {
	if k != "" && isBareKey(k) {
		return k
	}
	return tomlBasicString(k)
}

func isBareKey(k string) bool {
	for _, r := range k {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func tomlKeyPath(segments []string) string {
	parts := make([]string, len(segments))
	for i, s := range segments {
		parts[i] = tomlKey(s)
	}
	return strings.Join(parts, ".")
}

// tomlBasicString renders s as a TOML basic string with minimal escapes.
func tomlBasicString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
