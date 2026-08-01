// In-package (package toml, not toml_test) for the decoded-value converter, the
// error wrapper and the writers' defensive arms. Encode's validate() pass makes
// the "invalid node type" branches unreachable from the public API, so they are
// exercised by calling the writers directly.
package toml

import (
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/aura-studio/structure/v2/format"
	"github.com/aura-studio/structure/v2/internal/nodetest"
	"github.com/aura-studio/structure/v2/node"
)

// badNode is not a legal Node carrier; the writers must reject it.
var badNode node.Node = int(1)

func TestTOMLRemainingBranches(t *testing.T) {
	// A mixed array (tables plus scalars) is not an array of tables, so it goes
	// out inline and exercises the inline-table writer.
	out, err := Encode(nodetest.OM("mixed", nodetest.Arr(nodetest.OM("a", int64(1)), int64(2)), "empty_inline", nodetest.Arr(nodetest.OM())))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "mixed = [{a = 1}, 2]") {
		t.Errorf("inline table: %s", out)
	}
	back, err := Parse(out)
	if err != nil || !node.Equal(back, nodetest.OM("mixed", nodetest.Arr(nodetest.OM("a", int64(1)), int64(2)), "empty_inline", nodetest.Arr(nodetest.OM())), false) {
		t.Fatalf("inline round trip: %v\n%s", err, out)
	}

	// In-range uint64 and *big.Int carriers, and the full escape table. The last
	// want is the six-character text backslash-u-0-0-0-1, not a control byte.
	esc := "a\bb\fc\rd\te" + string(rune(1))
	out, err = Encode(nodetest.OM("u", uint64(7), "big", big.NewInt(8), "esc", esc))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"u = 7", "big = 8", `\b`, `\f`, `\r`, `\t`, "\\u0001"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if back, err := Parse(out); err != nil {
		t.Fatal(err)
	} else if v, _ := back.(*node.OrderedMap).Get("esc"); v != esc {
		t.Errorf("escapes round-tripped to %q", v)
	}
	if _, err := Encode(nodetest.OM("big", (*big.Int)(nil))); err == nil {
		t.Error("nil *big.Int encoded")
	}

	// A table nested inside an array-of-tables element can still fail.
	if _, err := Encode(nodetest.OM("items", nodetest.Arr(nodetest.OM("bad", badNode)))); err == nil {
		t.Error("illegal carrier inside an array of tables encoded")
	}
	var b strings.Builder
	if err := writeValue(&b, badNode); err == nil {
		t.Error("writeValue accepted an illegal carrier")
	}
	if err := writeInlineTable(&b, nodetest.OM("k", badNode)); err == nil {
		t.Error("writeInlineTable accepted an illegal value")
	}

	// wrapErr falls back to a bare *format.ParseError for untyped errors.
	var pe *format.ParseError
	if err := wrapErr(errors.New("boom")); !errors.As(err, &pe) || pe.Format != format.TOML {
		t.Errorf("wrapErr = %v (%T)", err, err)
	}
}

func TestTOMLAnyToNodeCarriers(t *testing.T) {
	// anyToNode handles every carrier BurntSushi can decode, including the
	// containers it only produces for inline values.
	got, err := anyToNode([]any{
		nil, true, "s", int64(1), 2.5,
		map[string]any{"b": int64(1), "a": int64(2)},
		[]map[string]any{{"x": int64(1)}},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	items := got.([]node.Node)
	if len(items) != 7 {
		t.Fatalf("anyToNode = %#v", got)
	}
	if keys := items[5].(*node.OrderedMap).Keys(); strings.Join(keys, ",") != "a,b" {
		t.Errorf("inline table keys = %v, want sorted", keys)
	}
	if inner, _ := items[6].([]node.Node)[0].(*node.OrderedMap).Get("x"); inner != int64(1) {
		t.Errorf("[]map[string]any = %#v", items[6])
	}
	if _, err := anyToNode(int32(1), 1); err == nil {
		t.Error("anyToNode accepted an unknown decoded type")
	}
	// The depth guard fires at the root and through both container kinds.
	if _, err := anyToNode(map[string]any{"a": int64(1)}, node.MaxDepth+1); !errors.Is(err, node.ErrTooDeep) {
		t.Error("anyToNode ignored the depth guard")
	}
	if _, err := anyToNode([]any{[]any{int64(1)}}, node.MaxDepth); !errors.Is(err, node.ErrTooDeep) {
		t.Error("nested anyToNode ignored the depth guard")
	}
	if _, err := anyToNode([]map[string]any{{"a": map[string]any{"b": int64(1)}}}, node.MaxDepth); !errors.Is(err, node.ErrTooDeep) {
		t.Error("array-of-tables anyToNode ignored the depth guard")
	}
}

func TestTOMLReplayConflicts(t *testing.T) {
	// Deeply-dotted keys, sub-tables created implicitly, and arrays of tables
	// addressed through a parent header all flow through resolve().
	n, err := Parse(strings.Join([]string{
		`a.b.c = 1`,
		`[t]`,
		`u.v = 2`,
		`[[g]]`,
		`x = 1`,
		`[g.h]`,
		`y = 2`,
		`[[g.k]]`,
		`z = 3`,
		`[[g]]`,
		`x = 4`,
	}, "\n") + "\n")
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*node.OrderedMap)
	ab, _ := m.Get("a")
	b, _ := ab.(*node.OrderedMap).Get("b")
	if c, _ := b.(*node.OrderedMap).Get("c"); c != int64(1) {
		t.Errorf("a.b.c = %v", c)
	}
	g, _ := m.Get("g")
	gs := g.([]node.Node)
	if len(gs) != 2 {
		t.Fatalf("g = %#v", gs)
	}
	if _, ok := gs[1].(*node.OrderedMap).Get("h"); ok {
		t.Error("g[0].h leaked into g[1]")
	}

	// Every parse error the replay itself can raise still surfaces as an error.
	for _, input := range []string{
		"[[a]]\nx = 1\n[a.b]\ny = 2\n[a.b]\nz = 3\n",
		"a = 1\n[a.b]\nc = 2\n",
		"[[a]]\n[[a.b]]\nx = 1\n[a.b]\ny = 2\n",
	} {
		if _, err := Parse(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}
