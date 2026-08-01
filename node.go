package structure

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
)

// maxDepth caps nesting for every parser (aligned with the standard library's
// json maxNestingDepth and xml maxUnmarshalDepth of 10000). Stack overflow is
// an unrecoverable fatal error in Go, so parsers must enforce this explicitly.
const maxDepth = 10000

// validate checks that n is a well-formed Node rooted at a mapping or array,
// with legal scalar types and depth <= maxDepth.
func validate(n Node) error {
	switch n.(type) {
	case *OrderedMap, []Node:
		return checkNode(n, 1)
	default:
		return fmt.Errorf("%w: got %T", ErrTopLevelScalar, n)
	}
}

// checkNode recursively validates types and depth. depth is the level of n
// itself (root = 1).
func checkNode(n Node, depth int) error {
	if depth > maxDepth {
		return ErrTooDeep
	}
	switch v := n.(type) {
	case *OrderedMap:
		if v == nil {
			return fmt.Errorf("structure: invalid node: nil *OrderedMap")
		}
		for key, val := range v.All() {
			if !utf8.ValidString(key) {
				return fmt.Errorf("structure: invalid node: mapping key is not valid UTF-8")
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
			return fmt.Errorf("structure: invalid node: string is not valid UTF-8")
		}
	case nil, bool:
	case int64, uint64, float64:
	case *big.Int:
		if v == nil {
			return fmt.Errorf("structure: invalid node: nil *big.Int")
		}
	default:
		return fmt.Errorf("structure: invalid node type %T", n)
	}
	return nil
}

// depthError returns ErrTooDeep when entering a container at level depth+1
// would exceed maxDepth; parsers call it before recursing.
func tooDeep(depth int) error {
	if depth > maxDepth {
		return ErrTooDeep
	}
	return nil
}

// nodeClone deep-copies a Node tree. Scalars are immutable; *big.Int and all
// containers are copied.
func nodeClone(n Node) Node {
	switch v := n.(type) {
	case *OrderedMap:
		if v == nil {
			return v
		}
		return v.Clone()
	case []Node:
		c := make([]Node, len(v))
		for i, e := range v {
			c[i] = nodeClone(e)
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

// nodeEqual deep-compares two Node trees. When ordered is true, mapping keys
// must appear in the same order (JSON/YAML/Python/JS round-trips); when
// false, order is irrelevant (TOML/XML/Lua, whose order is not semantically
// significant). Floats compare bit-exactly with NaN == NaN; int64/uint64/
// *big.Int compare numerically across carriers.
func nodeEqual(a, b Node, ordered bool) bool {
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
				if !nodeEqual(avv, bvv, ordered) {
					return false
				}
			}
			return true
		}
		for k, avv := range av.All() {
			bvv, ok := bv.Get(k)
			if !ok || !nodeEqual(avv, bvv, ordered) {
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
			if !nodeEqual(av[i], bv[i], ordered) {
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
	ab, aok := asBigInt(a)
	bb, bok := asBigInt(b)
	if !aok || !bok {
		return false
	}
	return ab.Cmp(bb) == 0
}

func asBigInt(n Node) (*big.Int, bool) {
	switch v := n.(type) {
	case int64:
		return big.NewInt(v), true
	case uint64:
		return new(big.Int).SetUint64(v), true
	case *big.Int:
		if v == nil {
			return nil, false
		}
		return v, true
	}
	return nil, false
}

// numberNode parses a numeric literal into the int64 -> uint64 -> *big.Int ->
// float64 carrier ladder. base is 10 for JSON, 0 (auto 0x/0o/0b) for YAML and
// Lua numeric tokens.
func numberNode(s string, base int) (Node, error) {
	if i, err := strconv.ParseInt(s, base, 64); err == nil {
		return i, nil
	}
	if u, err := strconv.ParseUint(s, base, 64); err == nil {
		return u, nil
	}
	if b, ok := new(big.Int).SetString(s, base); ok {
		return b, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, fmt.Errorf("structure: invalid number %q", s)
	}
	return f, nil
}

// formatFloat renders a float64 in its shortest round-trippable form,
// appending ".0" to integral values so the float type survives the round
// trip. NaN and ±Inf are an error (callers that support them — YAML, TOML —
// emit their own spellings before calling).
func formatFloat(f float64) (string, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", fmt.Errorf("%w: NaN/Inf has no literal in this format", ErrUnsupportedStructure)
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s, nil
}

// formatNumber renders any numeric Node to text. NaN/Inf float64 is an error.
func formatNumber(n Node) (string, error) {
	switch v := n.(type) {
	case int64:
		return strconv.FormatInt(v, 10), nil
	case uint64:
		return strconv.FormatUint(v, 10), nil
	case *big.Int:
		if v == nil {
			return "", fmt.Errorf("structure: invalid node: nil *big.Int")
		}
		return v.String(), nil
	case float64:
		return formatFloat(v)
	}
	return "", fmt.Errorf("structure: invalid node type %T", n)
}

// isNumeric reports whether n is one of the numeric carrier types.
func isNumeric(n Node) bool {
	switch n.(type) {
	case int64, uint64, *big.Int, float64:
		return true
	}
	return false
}

// isIntegerCarrier reports whether n rides one of the three integer carriers.
// numberNode falls back to float64 for text it cannot read as an integer, so
// callers that require an integer check the result with this.
func isIntegerCarrier(n Node) bool {
	switch n.(type) {
	case int64, uint64, *big.Int:
		return true
	}
	return false
}

// jsonEscape renders s as a JSON string literal (double-quoted, minimal
// escapes). U+2028/U+2029 are always escaped so the output is also safe to
// embed in pre-ES2019 JavaScript. Shared by the JSON and JS encoders and by
// OrderedMap.MarshalJSON.
func jsonEscape(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\u2028':
			b.WriteString(`\u2028`)
		case '\u2029':
			b.WriteString(`\u2029`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
