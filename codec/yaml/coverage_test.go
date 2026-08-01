package yaml_test

import (
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/aura-studio/structure/v2/codec/yaml"
	"github.com/aura-studio/structure/v2/internal/nodetest"
	"github.com/aura-studio/structure/v2/node"
)

func TestYAMLRemainingBranches(t *testing.T) {
	n, err := yaml.Parse(strings.Join([]string{
		`empty_map: {}`,
		`empty_seq: []`,
		`null_tilde: ~`,
		`null_bare:`,
		`quoted: "1"`,
		`neg: -5`,
		`oct: 0o17`,
		`hex: 0x1f`,
		`exp: 1.2e3`,
		`upper_bool: TRUE`,
		`false_upper: False`,
		`plus_inf: +.inf`,
		`nested: {a: {b: [1, 2]}}`,
	}, "\n") + "\n")
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*node.OrderedMap)
	for k, want := range map[string]node.Node{
		"quoted": "1", "neg": int64(-5), "oct": int64(15), "hex": int64(31),
		"exp": 1200.0, "upper_bool": true, "false_upper": false, "plus_inf": math.Inf(1),
	} {
		if v, _ := m.Get(k); v != want {
			t.Errorf("%s = %v (%T), want %v", k, v, v, want)
		}
	}
	for _, k := range []string{"null_tilde", "null_bare"} {
		if v, _ := m.Get(k); v != nil {
			t.Errorf("%s = %v, want nil", k, v)
		}
	}
	if v, _ := m.Get("empty_map"); v.(*node.OrderedMap).Len() != 0 {
		t.Error("empty_map not empty")
	}
	if v, _ := m.Get("empty_seq"); len(v.([]node.Node)) != 0 {
		t.Error("empty_seq not empty")
	}

	for _, input := range []string{
		"a: 1\n b: 2\n", "[1, 2", "{a: 1", "a: !!int notanint\n",
		"a: !!float notafloat\n", "? [1]\n: 2\n", "1: x\n", "a: !!set {x}\n",
	} {
		if _, err := yaml.Parse(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	// Non-container roots are rejected.
	for _, input := range []string{"5\n", "just a string\n", "true\n", "null\n"} {
		if _, err := yaml.Parse(input); !errors.Is(err, node.ErrTopLevelScalar) {
			t.Errorf("Parse(%q) err = %v, want ErrTopLevelScalar", input, err)
		}
	}
	// Encoding covers every carrier, including nested empties.
	out, err := yaml.Encode(nodetest.OM("u", uint64(math.MaxUint64), "big", new(big.Int).Lsh(big.NewInt(1), 80),
		"empty_map", nodetest.OM(), "empty_arr", nodetest.Arr(), "nil", nil, "arr", nodetest.Arr(int64(1), nodetest.OM("k", "v"))))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"18446744073709551615", "1208925819614629174706176", "{}", "[]", "null"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
}

// yaml.v3's own depth cap fires before ours; the error must still be the
// sentinel the module documents.
func TestYAMLDepthReturnsErrTooDeep(t *testing.T) {
	for name, input := range map[string]string{
		"flow sequence": strings.Repeat("[", node.MaxDepth+1) + strings.Repeat("]", node.MaxDepth+1),
		"flow mapping":  strings.Repeat("{a: ", node.MaxDepth+1) + "1" + strings.Repeat("}", node.MaxDepth+1),
	} {
		if _, err := yaml.Parse(input); !errors.Is(err, node.ErrTooDeep) {
			t.Errorf("%s: err = %v, want ErrTooDeep", name, err)
		}
	}
	// Within the limit still parses.
	if _, err := yaml.Parse(strings.Repeat("[", 1000) + strings.Repeat("]", 1000)); err != nil {
		t.Errorf("1000 levels rejected: %v", err)
	}
}

// yaml.v3 picks a scalar style that silently loses data for three kinds of
// string; the encoder must force double quotes for them.
func TestYAMLStringStylesRoundTrip(t *testing.T) {
	for _, s := range []string{
		"\nx", "\n", "\n\n", "x\n", "a\n\tb", "a\tb", "\ttab", "a\rb",
		"<<", "true", "null", "", " lead", "trail ", "a: b", "- x", "#c", "~",
	} {
		out, err := yaml.Encode(nodetest.OM("k", s))
		if err != nil {
			t.Errorf("Encode(%q): %v", s, err)
			continue
		}
		back, err := yaml.Parse(out)
		if err != nil {
			t.Errorf("Parse of encoded %q failed: %v\nencoded: %q", s, err, out)
			continue
		}
		got, _ := back.(*node.OrderedMap).Get("k")
		if gs, ok := got.(string); !ok || gs != s {
			t.Errorf("%q round-tripped to %#v (encoded %q)", s, got, out)
		}
	}
	// "<<" also has to survive in key position.
	out, err := yaml.Encode(nodetest.OM("<<", "v"))
	if err != nil {
		t.Fatal(err)
	}
	back, err := yaml.Parse(out)
	if err != nil {
		t.Fatalf("encoded %q does not re-parse: %v", out, err)
	}
	if v, ok := back.(*node.OrderedMap).Get("<<"); !ok || v != "v" {
		t.Errorf(`"<<" key round-tripped to %#v (encoded %q)`, v, out)
	}
}

// Only a plain << is a merge key; a quoted "<<" is an ordinary string key.
func TestYAMLMergeKeyOnlyWhenPlain(t *testing.T) {
	for _, input := range []string{"<<: {a: 1}\nb: 2\n", "<<: *ref\n"} {
		if _, err := yaml.Parse(input); err == nil {
			t.Errorf("merge key accepted: %q", input)
		}
	}
	for _, input := range []string{`"<<": v` + "\n", `'<<': v` + "\n"} {
		n, err := yaml.Parse(input)
		if err != nil {
			t.Errorf("quoted merge-looking key rejected: %q: %v", input, err)
			continue
		}
		if v, ok := n.(*node.OrderedMap).Get("<<"); !ok || v != "v" {
			t.Errorf("%q -> %#v", input, n)
		}
	}
	// A plain << in *value* position is yaml.v3's !!merge tag too, and is not a
	// key, so it must be rejected rather than silently mistyped.
	if _, err := yaml.Parse("k: <<\n"); err == nil {
		t.Error("plain << value accepted")
	}
	if n, err := yaml.Parse("k: \"<<\"\n"); err != nil {
		t.Errorf("quoted << value rejected: %v", err)
	} else if v, _ := n.(*node.OrderedMap).Get("k"); v != "<<" {
		t.Errorf(`quoted "<<" value -> %#v`, v)
	}
}

// An explicit tag is authoritative: the big-integer recovery that compensates
// for yaml.v3's !!float degradation must not retype a hand-written !!float.
func TestYAMLExplicitTagsAreHonoured(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  node.Node
	}{
		{"a: !!float 1\n", 1.0},
		{"a: !!float 1.5\n", 1.5},
		{"a: !!float -3\n", -3.0},
		{"a: !!int 7\n", int64(7)},
		{"a: !!str 1\n", "1"},
		{"a: !!null\n", nil},
		{"a: !!null ~\n", nil},
		// Implicit degradation is still recovered as an exact integer.
		{"a: 18446744073709551616\n", new(big.Int).Lsh(big.NewInt(1), 64)},
	} {
		n, err := yaml.Parse(tc.input)
		if err != nil {
			t.Errorf("%q: %v", tc.input, err)
			continue
		}
		got, _ := n.(*node.OrderedMap).Get("a")
		if !node.Equal(got, tc.want, true) {
			t.Errorf("%q -> %T %#v, want %T %#v", tc.input, got, got, tc.want, tc.want)
		}
	}
	// A tag that contradicts its content is an error, not a silent coercion.
	for _, input := range []string{"a: !!int 1.5\n", "a: !!int x\n", "a: !!null x\n"} {
		if _, err := yaml.Parse(input); err == nil {
			t.Errorf("contradictory tag accepted: %q", input)
		}
	}
}
