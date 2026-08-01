package lua_test

import (
	"errors"
	"math/big"
	"testing"

	"github.com/aura-studio/structure/v2/codec/lua"
	"github.com/aura-studio/structure/v2/internal/nodetest"
	"github.com/aura-studio/structure/v2/node"
)

// Regressions for defects found by the adversarial audit. Each test names the
// behaviour that used to be wrong, so a future change that reintroduces it fails
// here rather than silently corrupting data.

// Lua source cannot carry an integer beyond int64 (a decimal literal that
// overflows is read back as a float), so the encoder rejects it instead of
// emitting digits that read back as a different value.
func TestLuaRejectsIntegersBeyondInt64(t *testing.T) {
	for name, n := range map[string]node.Node{
		"uint64 above MaxInt64": nodetest.OM("k", uint64(1)<<63),
		"big.Int 2^64":          nodetest.OM("k", new(big.Int).Lsh(big.NewInt(1), 64)),
		"big.Int 2^100":         nodetest.OM("k", new(big.Int).Lsh(big.NewInt(1), 100)),
		"nested in array":       nodetest.OM("k", nodetest.Arr(new(big.Int).Lsh(big.NewInt(1), 70))),
	} {
		if _, err := lua.Encode(n); !errors.Is(err, node.ErrUnsupportedStructure) {
			t.Errorf("%s: err = %v, want ErrUnsupportedStructure", name, err)
		}
	}
	// Everything up to MaxInt64 is fine, including the boundary.
	for name, n := range map[string]node.Node{
		"MaxInt64":             nodetest.OM("k", int64(1)<<62),
		"uint64 within range":  nodetest.OM("k", uint64(7)),
		"big.Int within int64": nodetest.OM("k", big.NewInt(9007199254740993)),
	} {
		out, err := lua.Encode(n)
		if err != nil {
			t.Errorf("%s rejected: %v", name, err)
			continue
		}
		back, err := lua.Parse(out)
		if err != nil {
			t.Errorf("%s: encoded %q does not re-parse: %v", name, out, err)
			continue
		}
		if !node.Equal(n, back, false) {
			t.Errorf("%s round-tripped to %#v (encoded %q)", name, back, out)
		}
	}
}

// Lua's decimal \ddd escape consumes up to three digits, so a one-digit \0
// followed by a digit read back as a different byte.
func TestLuaNULEscapeIsThreeDigits(t *testing.T) {
	for _, s := range []string{
		"x\x00y", "x\x005", "\x00", "\x000", "\x009\x001",
		"a\x01" + "2", "\x1f9", "\x7f0",
	} {
		out, err := lua.Encode(nodetest.OM("k", s))
		if err != nil {
			t.Errorf("Encode(%q): %v", s, err)
			continue
		}
		back, err := lua.Parse(out)
		if err != nil {
			t.Errorf("encoded %q does not re-parse: %v (%q)", s, err, out)
			continue
		}
		if v, _ := back.(*node.OrderedMap).Get("k"); v != s {
			t.Errorf("%q round-tripped to %q (encoded %q)", s, v, out)
		}
	}
}

// Lua has one empty table constructor, and it reads back as a map — so an
// empty array cannot be encoded without silently changing shape.
func TestLuaRejectsEmptyArray(t *testing.T) {
	for name, n := range map[string]node.Node{
		"root":            nodetest.Arr(),
		"map value":       nodetest.OM("k", nodetest.Arr()),
		"nested in array": nodetest.Arr(nodetest.Arr(int64(1)), nodetest.Arr()),
	} {
		if _, err := lua.Encode(n); !errors.Is(err, node.ErrUnsupportedStructure) {
			t.Errorf("%s: err = %v, want ErrUnsupportedStructure", name, err)
		}
	}
	// A non-empty array still round-trips, and an empty *map* is still fine.
	for name, n := range map[string]node.Node{
		"non-empty array": nodetest.OM("k", nodetest.Arr(int64(1))),
		"empty map":       nodetest.OM("k", node.NewOrderedMap()),
	} {
		out, err := lua.Encode(n)
		if err != nil {
			t.Errorf("%s rejected: %v", name, err)
			continue
		}
		back, err := lua.Parse(out)
		if err != nil {
			t.Errorf("%s: encoded %q does not re-parse: %v", name, out, err)
			continue
		}
		if !node.Equal(n, back, false) {
			t.Errorf("%s round-tripped to %#v (encoded %q)", name, back, out)
		}
	}
}
