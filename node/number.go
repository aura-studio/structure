package node

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// NumberNode parses a numeric literal into the int64 -> uint64 -> *big.Int ->
// float64 carrier ladder. base is 10 for decimal text, 0 to auto-detect
// 0x/0o/0b prefixes (YAML and Lua numeric tokens).
func NumberNode(s string, base int) (Node, error) {
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
		return nil, invalidf("invalid number %q", s)
	}
	return f, nil
}

// BigResult normalizes a big.Int back onto the int64 -> uint64 -> *big.Int
// ladder, so a recovered value carries the same type as a directly parsed one.
func BigResult(b *big.Int) Node {
	if b.IsInt64() {
		return b.Int64()
	}
	if b.IsUint64() {
		return b.Uint64()
	}
	return b
}

// AsBigInt views an integer-carrying node as a *big.Int, reporting false for
// anything that is not one of the three integer carriers.
func AsBigInt(n Node) (*big.Int, bool) {
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

// Negate negates a numeric node, reporting false when n is not numeric. It is
// pure carrier arithmetic: the parsers that accept a leading sign (Lua, JS)
// build their own format-attributed error on the false branch, so this package
// stays free of any format dependency.
func Negate(n Node) (Node, bool) {
	switch v := n.(type) {
	case int64:
		b := big.NewInt(v)
		return BigResult(b.Neg(b)), true
	case uint64:
		b := new(big.Int).SetUint64(v)
		return BigResult(b.Neg(b)), true
	case *big.Int:
		if v == nil {
			return nil, false
		}
		return BigResult(new(big.Int).Neg(v)), true
	case float64:
		return -v, true
	}
	return nil, false
}

// FormatFloat renders a float64 in its shortest round-trippable form,
// appending ".0" to integral values so the float type survives the round
// trip. NaN and ±Inf are an error (callers that support them — YAML, TOML —
// emit their own spellings before calling).
func FormatFloat(f float64) (string, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		// Plain fmt.Errorf: the sentinel already carries MsgPrefix (see invalidf).
		return "", fmt.Errorf("%w: NaN/Inf has no literal in this format", ErrUnsupportedStructure)
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s, nil
}

// FormatNumber renders any numeric Node to text. NaN/Inf float64 is an error.
func FormatNumber(n Node) (string, error) {
	switch v := n.(type) {
	case int64:
		return strconv.FormatInt(v, 10), nil
	case uint64:
		return strconv.FormatUint(v, 10), nil
	case *big.Int:
		if v == nil {
			return "", invalidf("invalid node: nil *big.Int")
		}
		return v.String(), nil
	case float64:
		return FormatFloat(v)
	}
	return "", invalidf("invalid node type %T", n)
}

// IsNumeric reports whether n is one of the numeric carrier types.
func IsNumeric(n Node) bool {
	switch n.(type) {
	case int64, uint64, *big.Int, float64:
		return true
	}
	return false
}

// IsIntegerCarrier reports whether n rides one of the three integer carriers.
// NumberNode falls back to float64 for text it cannot read as an integer, so
// callers that require an integer check the result with this.
func IsIntegerCarrier(n Node) bool {
	switch n.(type) {
	case int64, uint64, *big.Int:
		return true
	}
	return false
}
