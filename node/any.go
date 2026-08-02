package node

import (
	"encoding/json"
	"iter"
	"math"
	"math/big"
	"sort"
)

// FromAny converts a plain Go value tree — the shape json.Unmarshal produces,
// map[string]any and []any all the way down — into a Node built from the
// carriers this package recognizes. ToAny converts back.
//
// These two functions are the only place in the module that touches native Go
// containers. Parse and Encode are unchanged: Encode still rejects a bare
// map[string]any, and Validate still rejects one inside a tree. Conversion is
// something the caller does at the boundary, before Encode or after Parse, so
// the internal model keeps exactly one representation for a mapping.
//
// # Key order
//
// A Go map has no order, so FromAny sorts its keys with sort.Strings rather than
// ranging over them: five of the six encoders emit *OrderedMap in insertion
// order, and a bare range would make their output differ between runs of the same
// program. Sorting trades an order the input never had for a deterministic one.
// That much is not optional, and no option changes it.
//
// An *OrderedMap in the input is the case that has a choice to make, since it
// does carry an order, and the two modes answer it differently:
//
//	FromAny(v)               sort it too — canonical form
//	FromAny(v, KeepOrder())  keep its insertion order — faithful form
//
// The default canonicalizes: equivalent data converts to identical text whatever
// its source, so a mapping that happened to arrive ordered cannot leak that order
// into the output. [KeepOrder] is for when the input's order is information — a
// hand-written config being rewritten, say — and the output should read the way
// the author wrote it.
//
// The rules apply per level, not per call, so a mixed tree gets both: under
// KeepOrder an *OrderedMap holding a Go map keeps its own order while that inner
// map still sorts.
//
// Neither mode can promise the order reaches the text. TOML and Lua do not
// specify table order, and their encoders may reorder keys regardless of what the
// Node holds; format.PreservesOrder reports which of the six formats carry order
// through.
//
// FromAny guarantees the result uses only legal carriers, nests no deeper than
// MaxDepth, and holds no typed-nil pointers. It does NOT check UTF-8 or the
// container-root rule; those stay with Validate, which Encode calls anyway.
//
// The input must be a tree. A cycle is caught (as ErrTooDeep), but a shared
// acyclic subtree — a DAG — is not: every reference is expanded, so N levels of
// a map holding the same child twice cost 2^N. Nothing detects that; deduplicate
// before calling if your value graph shares structure.
func FromAny(v any, opts ...Option) (Node, error) {
	return fromAny(v, 1, newOptions(opts))
}

// pairsFor iterates a mapping's entries, sorted by key or in insertion order.
//
// Keys returns a freshly allocated slice, so sorting it cannot disturb the
// caller's *OrderedMap — the input stays untouched in either mode. The unsorted
// path returns All directly and allocates nothing, keeping KeepOrder on the same
// code path the package had before modes existed.
func pairsFor(m *OrderedMap, sorted bool) iter.Seq2[string, Node] {
	if !sorted {
		return m.All()
	}
	return func(yield func(string, Node) bool) {
		keys := m.Keys()
		sort.Strings(keys)
		for _, k := range keys {
			// Get cannot miss: the keys came from this same map, and OrderedMap
			// holds each key once.
			val, _ := m.Get(k)
			if !yield(k, val) {
				return
			}
		}
	}
}

// fromAny converts one value. depth is the level of v itself (root = 1),
// matching checkNode. The guard is the first statement because Go's stack
// overflow is fatal and cannot be recovered: a 200k-deep map[string]any built
// in a loop has to fail as ErrTooDeep here, not take the process down. Until
// this function existed the root guard in Validate never saw such a tree,
// because it does not recognize map[string]any as a root at all.
//
// A self-referencing map needs no visited set: depth rises monotonically, so a
// cycle hits MaxDepth and returns a recoverable error. A shared (acyclic)
// subtree is a different matter — see the DAG note on FromAny's doc.
func fromAny(v any, depth int, cfg options) (Node, error) {
	if err := TooDeep(depth); err != nil {
		return nil, err
	}
	switch v := v.(type) {
	case nil:
		return nil, nil
	case bool:
		return v, nil
	case string:
		return v, nil

	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		m := NewOrderedMap()
		for _, k := range keys {
			child, err := fromAny(v[k], depth+1, cfg)
			if err != nil {
				return nil, err
			}
			m.Set(k, child)
		}
		return m, nil

	// []Node and []any are the same type, since Node is an alias for any. A new
	// slice is allocated rather than written back in place: the caller still
	// owns the input, and elements converted in place would mutate it.
	case []Node:
		out := make([]Node, len(v))
		for i, e := range v {
			child, err := fromAny(e, depth+1, cfg)
			if err != nil {
				return nil, err
			}
			out[i] = child
		}
		return out, nil

	// Already a mapping, but its values may still be native containers. This is
	// the one place a mode changes anything: the map's own keys are sorted by
	// default and left in insertion order under KeepOrder. A Go map's keys are
	// sorted either way, since it has no order to keep.
	case *OrderedMap:
		if v == nil {
			return nil, invalidf("invalid node: nil *OrderedMap")
		}
		m := NewOrderedMap()
		for k, val := range pairsFor(v, !cfg.keepOrder) {
			child, err := fromAny(val, depth+1, cfg)
			if err != nil {
				return nil, err
			}
			m.Set(k, child)
		}
		return m, nil

	// int is here because it is what an untyped constant defaults to: without
	// it, FromAny(map[string]any{"a": 1}) would fail and the feature would be
	// useless in the exact form callers write by hand.
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil

	// The unsigned side goes through uint64, never int64: int64(uint(1<<63))
	// is silently negative. uint8/16/32 always fit, so they convert directly.
	case uint:
		return fromUint(uint64(v)), nil
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		return fromUint(v), nil

	// float32 widens exactly, but the value it carries was already rounded to
	// 24-bit mantissa: float32(0.1) reads back as 0.10000000149011612. That
	// artifact belongs to the input, not to the conversion.
	case float32:
		return float64(v), nil
	case float64:
		return v, nil

	// Copied, then renormalized onto the ladder so a big.Int holding a small
	// value carries the same type as a parsed one. The copy matters because a
	// *big.Int the caller keeps is mutable shared state.
	case *big.Int:
		if v == nil {
			return nil, invalidf("invalid node: nil *big.Int")
		}
		return BigResult(new(big.Int).Set(v)), nil

	// json.Number is what Decoder.UseNumber produces, which is the one way to
	// get full integer precision out of encoding/json — the same reason this
	// module has a carrier ladder. It is a named string type, so "case string"
	// above does not match it.
	case json.Number:
		return NumberNode(string(v), 10)

	// Everything else is rejected by name. There is deliberately no
	// fmt.Stringer or error fallback: *big.Int, json.Number, time.Time,
	// big.Float, time.Duration and net.IP all implement String(), so a
	// catch-all would turn 1<<100 from a correct integer into a quoted string
	// that then passes Validate — a silent, plausible-looking wrong answer.
	// Named types, typed maps (map[any]any, map[string]string), typed slices
	// ([]string, []byte), arrays, structs, pointers, chan, func, complex and
	// uintptr all land here. Convert them yourself; the choice of
	// representation (a time layout, a base64 spelling) is not this package's
	// to make.
	default:
		return nil, invalidf("cannot convert %T to a node", v)
	}
}

// fromUint places an unsigned value on the ladder, preferring int64 when it
// fits, the way NumberNode does for text.
func fromUint(u uint64) Node {
	if u <= math.MaxInt64 {
		return int64(u)
	}
	return u
}

// ToAny converts a Node tree into plain Go values: *OrderedMap becomes
// map[string]any and []Node becomes []any, so the result can go straight into
// encoding/json, a template, or anything reflecting over native containers.
// Scalars pass through as their carrier type; *big.Int is copied.
//
// # Key order
//
// Mapping order is lost, and unlike FromAny there is nothing to trade it for: a
// Go map cannot hold it. So when order matters, do not come this way at all —
// keep the Node. Encode takes one directly, and so does json.Marshal, through
// OrderedMap.MarshalJSON:
//
//	Encode(n, JSON)        // ordered
//	json.Marshal(n)        // ordered
//	json.Marshal(ToAny(n)) // NOT ordered — the Go map dropped it
//
// KeepOrder is accepted here for symmetry with FromAny, and for whatever later
// option belongs on this side. It cannot produce an ordered Go map, because no
// such thing exists. What it means instead is "leave the mappings alone", which
// makes the call a deep copy and nothing more: for any valid Node it is exactly
// [Clone], and delegates to it rather than reimplementing it.
//
// n must be a valid Node — Parse output always is. Like Clone, Equal and
// MarshalNode, this function has no depth counter and no error return, so a
// tree deeper than MaxDepth or one containing a cycle overflows the stack.
// Validate rules both out.
func ToAny(n Node, opts ...Option) any {
	if newOptions(opts).keepOrder {
		return Clone(n)
	}
	return toAny(n)
}

// toAny is the conversion proper: every mapping becomes a map[string]any.
func toAny(n Node) any {
	switch v := n.(type) {
	case *OrderedMap:
		if v == nil {
			return nil
		}
		m := make(map[string]any, v.Len())
		for k, val := range v.All() {
			m[k] = toAny(val)
		}
		return m
	case []Node:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = toAny(e)
		}
		return out
	case *big.Int:
		if v == nil {
			return nil
		}
		return new(big.Int).Set(v)
	default:
		return v
	}
}
