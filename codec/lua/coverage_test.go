package lua_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/aura-studio/structure/v2/codec/lua"
	"github.com/aura-studio/structure/v2/format"
	"github.com/aura-studio/structure/v2/internal/nodetest"
	"github.com/aura-studio/structure/v2/node"
)

// Error branches and scalar-carrier edges the behavioural test does not reach.
// The carrier arithmetic itself (Negate, BigResult) is package node's; what is
// tested here is only how the Lua parser and encoder drive it.

func TestLuaErrorsAndScalarEdges(t *testing.T) {
	// A golua parse failure arrives as a positioned *format.ParseError.
	var pe *format.ParseError
	_, err := lua.Parse("{1,")
	if !errors.As(err, &pe) || pe.Format != format.Lua {
		t.Fatalf("Parse syntax error = %v (%T)", err, err)
	}

	// Negation across the carriers, reached through real source text.
	n, err := lua.Parse(`{ i = -1, f = -1.5, big = -9223372036854775808, z = -0.0 }`)
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*node.OrderedMap)
	if v, _ := m.Get("i"); v != int64(-1) {
		t.Errorf("i = %v (%T)", v, v)
	}
	if v, _ := m.Get("f"); v != -1.5 {
		t.Errorf("f = %v", v)
	}
	if v, _ := m.Get("big"); v != int64(math.MinInt64) {
		t.Errorf("big = %v (%T)", v, v)
	}

	// Rejected expression forms.
	for _, input := range []string{`{a = not true}`, `{a = #x}`, `{a = 1 + 1}`, `"x"`, `nil`, `{[true]=1}`} {
		if _, err := lua.Parse(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}

	// String escapes: control characters use decimal \ddd.
	out, err := lua.Encode(nodetest.OM("k", "a\"b\\c\nd\re\tf"+string(rune(0))+string(rune(1))+string(rune(0x7f))))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`\"`, `\\`, `\n`, `\r`, `\t`, `\0`, `\001`, `\127`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if _, err := lua.Encode(nodetest.OM("m", nodetest.OM("n", math.Inf(-1)))); !errors.Is(err, node.ErrUnsupportedStructure) {
		t.Error("nested -Inf encoded")
	}
	if _, err := lua.Encode(nodetest.Arr(nodetest.Arr(nil))); !errors.Is(err, node.ErrUnsupportedStructure) {
		t.Error("nested nil array element encoded")
	}
	// A map whose only entries are nil collapses to an empty table.
	if out, err := lua.Encode(nodetest.OM("gone", nil)); err != nil || !strings.Contains(out, "{}") {
		t.Fatalf("nil-only map = %q, %v", out, err)
	}
	// An empty array has no Lua spelling: {} reads back as an empty map, so the
	// encoder rejects it rather than silently changing the value's shape.
	if _, err := lua.Encode(nodetest.Arr()); !errors.Is(err, node.ErrUnsupportedStructure) {
		t.Errorf("empty array encoded: %v", err)
	}
}
