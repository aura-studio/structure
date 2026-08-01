package lua_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/aura-studio/structure/v2/codec/lua"
	"github.com/aura-studio/structure/v2/internal/nodetest"
	"github.com/aura-studio/structure/v2/node"
)

func TestLuaLiteralSubset(t *testing.T) {
	n, err := lua.Parse(`{ z = 1, ["a-b"] = "x\n", hex = 0x1f, long = [[ok]], gone = nil, z = 2 }`)
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*node.OrderedMap)
	z, _ := m.Get("z")
	if z != int64(2) {
		t.Fatal(z)
	}
	if _, ok := m.Get("gone"); ok {
		t.Fatal("nil key retained")
	}
	if a, err := lua.Parse(`{1, 2, 3,}`); err != nil || len(a.([]node.Node)) != 3 {
		t.Fatalf("%v %v", a, err)
	}
	for _, input := range []string{`1`, `{1,a=2}`, `{[1]=2}`, `function() end`, `f()`, `{nil}`} {
		if _, err := lua.Parse(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	out, err := lua.Encode(nodetest.OM("while", "x", "a-b", "y", "ok", int64(1), "nil", nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `["while"]`) || !strings.Contains(out, `["a-b"]`) || strings.Contains(out, "nil =") {
		t.Fatal(out)
	}
	if _, err := lua.Encode(nodetest.Arr(nil)); !errors.Is(err, node.ErrUnsupportedStructure) {
		t.Fatal(err)
	}
	if _, err := lua.Encode(nodetest.OM("n", math.NaN())); !errors.Is(err, node.ErrUnsupportedStructure) {
		t.Fatal(err)
	}
}
