// Package node holds the intermediate representation every codec in this
// module parses into and encodes from: the Node model and its ordered mapping
// type, the numeric carrier ladder, and the sentinels describing what the model
// itself rejects.
//
// It is one of the two roots of the module's import graph and deliberately
// depends on nothing else here — not even package format — so that anything
// operating purely on the data model (a future merge, a diff, a walker) can
// import it without dragging in parsers or format enums.
package node

import (
	"fmt"
	"math"
	"math/big"
	"unicode/utf8"
)

// MaxDepth caps nesting for every parser (aligned with the standard library's
// json maxNestingDepth and xml maxUnmarshalDepth of 10000). Stack overflow is
// an unrecoverable fatal error in Go, so parsers must enforce this explicitly.
const MaxDepth = 10000

// Validate checks that n is a well-formed Node rooted at a mapping or array,
// with legal scalar types and depth <= MaxDepth.
func Validate(n Node) error {
	switch n.(type) {
	case *OrderedMap, []Node:
		return checkNode(n, 1)
	default:
		// Plain fmt.Errorf, not invalidf: the wrapped sentinel already carries
		// MsgPrefix, and prepending a second one would produce
		// "structure: structure: top-level scalar ...".
		return fmt.Errorf("%w: got %T", ErrTopLevelScalar, n)
	}
}

// checkNode recursively validates types and depth. depth is the level of n
// itself (root = 1).
func checkNode(n Node, depth int) error {
	if depth > MaxDepth {
		return ErrTooDeep
	}
	switch v := n.(type) {
	case *OrderedMap:
		if v == nil {
			return invalidf("invalid node: nil *OrderedMap")
		}
		for key, val := range v.All() {
			if !utf8.ValidString(key) {
				return invalidf("invalid node: mapping key is not valid UTF-8")
			}
			if err := checkNode(val, depth+1); err != nil {
				return err
			}
		}
	case []Node:
		for _, e := range v {
			if err := checkNode(e, depth+1); err != nil {
				return err
			}
		}
	case string:
		// Every target format is UTF-8 text; the encoders range over runes and
		// would silently substitute U+FFFD for stray bytes.
		if !utf8.ValidString(v) {
			return invalidf("invalid node: string is not valid UTF-8")
		}
	case nil, bool:
	case int64, uint64, float64:
	case *big.Int:
		if v == nil {
			return invalidf("invalid node: nil *big.Int")
		}
	default:
		return invalidf("invalid node type %T", n)
	}
	return nil
}

// TooDeep returns ErrTooDeep when entering a container at level depth would
// exceed MaxDepth; parsers call it before recursing.
func TooDeep(depth int) error {
	if depth > MaxDepth {
		return ErrTooDeep
	}
	return nil
}

// Clone deep-copies a Node tree. Scalars are immutable; *big.Int and all
// containers are copied.
func Clone(n Node) Node {
	switch v := n.(type) {
	case *OrderedMap:
		if v == nil {
			return v
		}
		return v.Clone()
	case []Node:
		c := make([]Node, len(v))
		for i, e := range v {
			c[i] = Clone(e)
		}
		return c
	case *big.Int:
		if v == nil {
			return v
		}
		return new(big.Int).Set(v)
	default:
		return v
	}
}

// Equal deep-compares two Node trees. When ordered is true, mapping keys must
// appear in the same order (JSON/YAML/Python/JS round-trips); when false, order
// is irrelevant (TOML/XML/Lua, whose order is not semantically significant).
// Floats compare bit-exactly with NaN == NaN; int64/uint64/*big.Int compare
// numerically across carriers.
func Equal(a, b Node, ordered bool) bool {
	switch av := a.(type) {
	case nil:
		return b == nil
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case int64, uint64, *big.Int:
		return intEqual(a, b)
	case float64:
		bv, ok := b.(float64)
		if !ok {
			return false
		}
		if math.IsNaN(av) && math.IsNaN(bv) {
			return true
		}
		return math.Float64bits(av) == math.Float64bits(bv)
	case *OrderedMap:
		bv, ok := b.(*OrderedMap)
		if !ok || av.Len() != bv.Len() {
			return false
		}
		if ordered {
			ak, bk := av.Keys(), bv.Keys()
			for i := range ak {
				if ak[i] != bk[i] {
					return false
				}
			}
			for _, k := range ak {
				avv, _ := av.Get(k)
				bvv, _ := bv.Get(k)
				if !Equal(avv, bvv, ordered) {
					return false
				}
			}
			return true
		}
		for k, avv := range av.All() {
			bvv, ok := bv.Get(k)
			if !ok || !Equal(avv, bvv, ordered) {
				return false
			}
		}
		return true
	case []Node:
		bv, ok := b.([]Node)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !Equal(av[i], bv[i], ordered) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// intEqual reports whether two integer-carrying nodes are numerically equal.
func intEqual(a, b Node) bool {
	ab, aok := AsBigInt(a)
	bb, bok := AsBigInt(b)
	if !aok || !bok {
		return false
	}
	return ab.Cmp(bb) == 0
}
