// In-package (package lua, not lua_test) for the defensive arms of the writers
// and the error wrapper. Encode's validate() pass makes the "invalid node type"
// branches unreachable from the public API, so they are exercised by calling the
// writers directly. Importing internal/nodetest is safe: it depends on package
// node alone.
package lua

import (
	"errors"
	"strings"
	"testing"

	"github.com/aura-studio/structure/v2/format"
	"github.com/aura-studio/structure/v2/internal/nodetest"
	"github.com/aura-studio/structure/v2/node"
)

// badNode is not a legal Node carrier; the writers must reject it.
var badNode node.Node = int(1)

func TestLuaErrorAndWriterPaths(t *testing.T) {
	for name, input := range map[string]string{
		"hash then array":      `{a = 1, 2}`,
		"array element error":  `{ -"x" }`,
		"nested unary operand": `{a = - -"x"}`,
	} {
		if _, err := Parse(input); err == nil {
			t.Errorf("%s: accepted %q", name, input)
		}
	}
	// wrapErr falls back to a bare *format.ParseError for untyped errors.
	var pe *format.ParseError
	if err := wrapErr(errors.New("boom")); !errors.As(err, &pe) || pe.Format != format.Lua {
		t.Errorf("wrapErr = %v (%T)", err, err)
	}
	// Bare keys may contain digits after the first rune, but not lead with one.
	out, err := Encode(nodetest.OM("a1", int64(1), "1a", int64(2)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a1 = 1") || !strings.Contains(out, `["1a"] = 2`) {
		t.Errorf("bare-key handling: %s", out)
	}
	if bareKey("1a") || bareKey("") || bareKey("a-b") || bareKey("while") {
		t.Error("bareKey accepted a key that needs bracket form")
	}
	if !bareKey("a1") || !bareKey("_x") {
		t.Error("bareKey needlessly bracketed an identifier")
	}
	var b strings.Builder
	if err := writeValue(&b, nil, 0, false); err != nil {
		t.Fatalf("nil in hash position: %v", err)
	}
	if !strings.Contains(b.String(), "nil") {
		t.Errorf("nil rendered as %q", b.String())
	}
	if err := writeValue(&b, badNode, 0, false); err == nil {
		t.Error("writeValue accepted an illegal carrier")
	}
}
