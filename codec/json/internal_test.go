// In-package (package json, not json_test) because these are the defensive arms
// of the writers and the token/error helpers. Encode's validate() pass makes the
// "invalid node type" branches unreachable from the public API, so they are
// exercised by calling the writers directly. Importing internal/nodetest from
// here is safe: nodetest depends on package node alone.
package json

import (
	stdjson "encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aura-studio/structure/v2/format"
	"github.com/aura-studio/structure/v2/internal/nodetest"
	"github.com/aura-studio/structure/v2/node"
)

// badNode is not a legal Node carrier; the writers must reject it.
var badNode node.Node = int(1)

func TestJSONErrorAndWriterPaths(t *testing.T) {
	for name, input := range map[string]string{
		"garbage first token": `@`,
		"trailing garbage":    `{} }`,
		"trailing value":      `{} 1`,
	} {
		if _, err := Parse(input); err == nil {
			t.Errorf("%s: accepted %q", name, input)
		}
	}
	// tokenName covers every token kind it can be handed.
	for tok, want := range map[stdjson.Token]string{
		stdjson.Delim('{'):     `delimiter "{"`,
		stdjson.Number("1.5"):  "number 1.5",
		stdjson.Token("s"):     "string",
		stdjson.Token(true):    "boolean",
		stdjson.Token(nil):     "null",
		stdjson.Token(int(42)): "int",
	} {
		if got := tokenName(tok); got != want {
			t.Errorf("tokenName(%v) = %q, want %q", tok, got, want)
		}
	}
	// value rejects a stray closing delimiter and an unknown token type. A
	// parser with no decoder reports position 0,0 rather than panicking.
	p := &parser{}
	if _, err := p.value(stdjson.Delim('}'), 1); err == nil {
		t.Error("parser.value accepted a closing delimiter")
	}
	if _, err := p.value(stdjson.Token(int(1)), 1); err == nil {
		t.Error("parser.value accepted an unknown token")
	}
	// wrapErr maps the standard library's depth message onto ErrTooDeep, and
	// otherwise produces a JSON-attributed *format.ParseError.
	if err := wrapErr(errors.New("exceeded max depth")); !errors.Is(err, node.ErrTooDeep) {
		t.Errorf("wrapErr(depth) = %v, want ErrTooDeep", err)
	}
	var pe *format.ParseError
	if err := wrapErr(errors.New("boom")); !errors.As(err, &pe) || pe.Format != format.JSON {
		t.Errorf("wrapErr = %v (%T)", err, err)
	}
	// Empty containers at the root, and the writers' invalid-type arms.
	for _, tc := range []struct {
		n    node.Node
		want string
	}{{nodetest.OM(), "{}\n"}, {nodetest.Arr(), "[]\n"}} {
		if out, err := Encode(tc.n); err != nil || out != tc.want {
			t.Errorf("Encode(%#v) = %q, %v", tc.n, out, err)
		}
	}
	var b strings.Builder
	if err := writeValue(&b, badNode, 0); err == nil {
		t.Error("writeValue accepted an illegal carrier")
	}
	if err := writeArray(&b, nodetest.Arr(badNode), 0); err == nil {
		t.Error("writeArray accepted an illegal element")
	}
	if err := writeObject(&b, nodetest.OM("k", badNode), 0); err == nil {
		t.Error("writeObject accepted an illegal value")
	}
}
