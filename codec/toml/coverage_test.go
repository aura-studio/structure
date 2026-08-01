package toml_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/aura-studio/structure/v2/codec/toml"
	"github.com/aura-studio/structure/v2/format"
	"github.com/aura-studio/structure/v2/internal/nodetest"
	"github.com/aura-studio/structure/v2/node"
)

func TestTOMLValueAndTableShapes(t *testing.T) {
	input := strings.Join([]string{
		`str = "s"`,
		`multi = """a\nb"""`,
		`literal = 'raw\no'`,
		`int = -7`,
		`float = 1.5e3`,
		`sci = 6.0`,
		`nan = nan`,
		`inf = inf`,
		`ninf = -inf`,
		`bool = false`,
		`arr = [1, 2, 3]`,
		`mixed = ["a", 1, true]`,
		`nested_arr = [[1, 2], [3]]`,
		`inline = {a = 1, b = "x"}`,
		`inline_aot = [{a = 1}, {a = 2}]`,
		`empty_arr = []`,
		`[t]`,
		`x = 1`,
		`[t.sub]`,
		`y = 2`,
		`[[items]]`,
		`id = 1`,
		`[items.meta]`,
		`tag = "first"`,
		`[[items]]`,
		`id = 2`,
		`[items.meta]`,
		`tag = "second"`,
		`[[items.notes]]`,
		`text = "n1"`,
		`[[items.notes]]`,
		`text = "n2"`,
	}, "\n") + "\n"

	n, err := toml.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*node.OrderedMap)
	if v, _ := m.Get("nan"); !math.IsNaN(v.(float64)) {
		t.Errorf("nan = %v", v)
	}
	if v, _ := m.Get("inf"); v != math.Inf(1) {
		t.Errorf("inf = %v", v)
	}
	if v, _ := m.Get("ninf"); v != math.Inf(-1) {
		t.Errorf("ninf = %v", v)
	}
	inlineAoT, _ := m.Get("inline_aot")
	elems, ok := inlineAoT.([]node.Node)
	if !ok || len(elems) != 2 {
		t.Fatalf("inline_aot = %#v", inlineAoT)
	}
	if v, _ := elems[1].(*node.OrderedMap).Get("a"); v != int64(2) {
		t.Errorf("inline_aot[1].a = %v", v)
	}

	// Nested arrays of tables keep per-element identity and order.
	items, _ := m.Get("items")
	itemArr := items.([]node.Node)
	if len(itemArr) != 2 {
		t.Fatalf("items = %#v", itemArr)
	}
	second := itemArr[1].(*node.OrderedMap)
	meta, _ := second.Get("meta")
	if tag, _ := meta.(*node.OrderedMap).Get("tag"); tag != "second" {
		t.Errorf("items[1].meta.tag = %v", tag)
	}
	notes, _ := second.Get("notes")
	noteArr, ok := notes.([]node.Node)
	if !ok || len(noteArr) != 2 {
		t.Fatalf("items[1].notes = %#v", notes)
	}
	if text, _ := noteArr[0].(*node.OrderedMap).Get("text"); text != "n1" {
		t.Errorf("notes[0].text = %v", text)
	}
	first := itemArr[0].(*node.OrderedMap)
	if _, hasNotes := first.Get("notes"); hasNotes {
		t.Error("notes leaked into items[0]")
	}

	// Re-encoding and re-parsing preserves the whole shape.
	out, err := toml.Encode(n)
	if err != nil {
		t.Fatal(err)
	}
	back, err := toml.Parse(out)
	if err != nil || !node.Equal(n, back, false) {
		t.Fatalf("round trip: %v\n%s", err, out)
	}

	// Quoted keys survive both directions.
	quoted, err := toml.Encode(nodetest.OM("a b", int64(1), "dotted.key", nodetest.OM("x", int64(2)), "", int64(3)))
	if err != nil {
		t.Fatal(err)
	}
	if qback, err := toml.Parse(quoted); err != nil || qback.(*node.OrderedMap).Len() != 3 {
		t.Fatalf("quoted keys: %v\n%s", err, quoted)
	}
}

func TestTOMLErrorSurface(t *testing.T) {
	for _, input := range []string{
		"x = ", "[unclosed", "a = 1\na = 2\n", "= 1", "[[a]]\n[a]\nx=1\n",
		"a = 1\n[a]\nb = 2\n", "a = 0x", "a = [1,",
	} {
		if _, err := toml.Parse(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	var pe *format.ParseError
	if _, err := toml.Parse("x = ["); !errors.As(err, &pe) || pe.Format != format.TOML {
		t.Errorf("expected a TOML *ParseError, got %v (%T)", err, err)
	}
	// nan/inf reach TOML but not the other scalar-restricted targets.
	if _, err := toml.Encode(nodetest.OM("arr", nodetest.Arr(nil))); !errors.Is(err, node.ErrUnsupportedStructure) {
		t.Error("nil inside an array encoded")
	}
	if _, err := toml.Encode(nodetest.OM("t", nodetest.OM("u", uint64(math.MaxUint64)))); !errors.Is(err, node.ErrUnsupportedStructure) {
		t.Error("uint64 overflow encoded")
	}
	if _, err := toml.Encode(nodetest.OM("t", nodetest.OM("n", nil))); !errors.Is(err, node.ErrUnsupportedStructure) {
		t.Error("nested nil encoded")
	}
}

func TestTOMLTimeCarriers(t *testing.T) {
	n, err := toml.Parse(strings.Join([]string{
		`offset = 1979-05-27T07:32:00-08:00`,
		`utc = 1979-05-27T07:32:00Z`,
		`frac = 1979-05-27T00:32:00.999999Z`,
		`local_dt = 1979-05-27T07:32:00`,
		`local_d = 1979-05-27`,
		`local_t = 07:32:00.5`,
		`arr = [1979-05-27, 1979-05-28]`,
	}, "\n") + "\n")
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*node.OrderedMap)
	for k, want := range map[string]string{
		"offset":   "1979-05-27T07:32:00-08:00",
		"utc":      "1979-05-27T07:32:00Z",
		"local_dt": "1979-05-27T07:32:00",
		"local_d":  "1979-05-27",
		"local_t":  "07:32:00.5",
	} {
		if v, _ := m.Get(k); v != want {
			t.Errorf("%s = %v, want %s", k, v, want)
		}
	}
	timeArr, _ := m.Get("arr")
	if items := timeArr.([]node.Node); len(items) != 2 || items[0] != "1979-05-27" {
		t.Errorf("arr = %#v", timeArr)
	}
}

// The array-of-tables cursor key used to be built by concatenating "\x00"+seg
// and "#"+index, which is not injective: element 0 of a real [[a]] array and an
// ordinary quoted key "a#0" both encoded to "\x00a#0". The two unrelated nodes
// then shared one counter, silently dropping leaf values and growing phantom
// empty tables. No exotic input is required — "issue#0" is ordinary TOML. The
// encoding's own injectivity is pinned by an in-package test; this one checks
// the behaviour a user can observe.
func TestTOMLCursorKeyIsInjective(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  node.Node
	}{
		{
			"quoted key collides with element 0",
			"[[a]]\n[[a.b]]\nx = 1\n\n[\"a#0\"]\n[[\"a#0\".b]]\ny = 1\n",
			nodetest.OM("a", nodetest.Arr(nodetest.OM("b", nodetest.Arr(nodetest.OM("x", int64(1))))),
				"a#0", nodetest.OM("b", nodetest.Arr(nodetest.OM("y", int64(1))))),
		},
		{
			"collision with two elements",
			"[[a]]\n[[a.b]]\nx = 1\n\n[\"a#0\"]\n[[\"a#0\".b]]\ny = 1\n[[\"a#0\".b]]\ny = 2\n",
			nodetest.OM("a", nodetest.Arr(nodetest.OM("b", nodetest.Arr(nodetest.OM("x", int64(1))))),
				"a#0", nodetest.OM("b", nodetest.Arr(nodetest.OM("y", int64(1)), nodetest.OM("y", int64(2))))),
		},
		{
			"realistic issue#0 key",
			"[[issue]]\n[[issue.tag]]\nn = \"p\"\n\n[\"issue#0\"]\n[[\"issue#0\".tag]]\nn = \"q\"\n",
			nodetest.OM("issue", nodetest.Arr(nodetest.OM("tag", nodetest.Arr(nodetest.OM("n", "p")))),
				"issue#0", nodetest.OM("tag", nodetest.Arr(nodetest.OM("n", "q")))),
		},
		{
			// Reversed source order: the damage used to land on the genuine array.
			"reversed order",
			"[\"a#0\"]\n[[\"a#0\".b]]\ny = 1\n\n[[a]]\n[[a.b]]\nx = 1\n",
			nodetest.OM("a#0", nodetest.OM("b", nodetest.Arr(nodetest.OM("y", int64(1)))),
				"a", nodetest.Arr(nodetest.OM("b", nodetest.Arr(nodetest.OM("x", int64(1)))))),
		},
	} {
		got, err := toml.Parse(tc.input)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if !node.Equal(got, tc.want, false) {
			t.Errorf("%s:\n got  %#v\n want %#v", tc.name, got, tc.want)
		}
	}
}
