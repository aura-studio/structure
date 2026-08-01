package python_test

import (
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/aura-studio/structure/v2/codec/python"
	"github.com/aura-studio/structure/v2/internal/nodetest"
	"github.com/aura-studio/structure/v2/node"
)

func TestPythonEscapesAndNumberEdges(t *testing.T) {
	input := `{"esc": "\a\b\f\v\r\n\t\\\'\"\0", "oct": "\101\377", "hex": "\x41", "u": "A", "U": "\U00000041"}`
	n, err := python.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*node.OrderedMap)
	esc, _ := m.Get("esc")
	want := "\a\b\f\v\r\n\t\\'\"" + string(rune(0))
	if esc != want {
		t.Errorf("esc = %q, want %q", esc, want)
	}
	if v, _ := m.Get("oct"); v != "A"+string(rune(0xff)) {
		t.Errorf("oct = %q", v)
	}
	for _, k := range []string{"hex", "u", "U"} {
		if v, _ := m.Get(k); v != "A" {
			t.Errorf("%s = %q, want A", k, v)
		}
	}

	// Line continuation drops the newline.
	cont, err := python.Parse("{\"a\": \"x\\\ny\"}")
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := cont.(*node.OrderedMap).Get("a"); v != "xy" {
		t.Errorf("line continuation = %q, want %q", v, "xy")
	}

	// Comments and surrounding whitespace are skipped.
	if _, err := python.Parse("# lead\n{ \"a\": 1, # trailing\n \"b\": 2 }\n# end\n"); err != nil {
		t.Fatal(err)
	}

	for _, bad := range []string{
		`{"a": "\q"}`, `{"a": "\x4"}`, `{"a": "\xZZ"}`, `{"a": "\u00"}`,
		`{"a": "\400"}`, `{"a": "unterminated}`, "{\"a\": \"nl\nnl\"}",
		`{"a": 1`, `{"a" 1}`, `{"a": 1 "b": 2}`, `[1 2]`, `(1 2)`, `{`, `[`, `(`,
		`{"a": .}`, `{"a": +}`, `{"a": -}`, `{"a": 0x}`, `"x" "y"`, `{"a": inf}`,
		`{"a": nan}`, `{"a": 1_}`, `{"a": _1}`, `{"a": 0b12}`, `{"a": u"x" "y"}`,
	} {
		if _, err := python.Parse(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}

	// Underscore separators are legal between digits in every base.
	ok, err := python.Parse(`{"d": 1_000, "h": 0x_1f, "o": 0o1_7, "b": 0b1_1, "f": 1_0.5e1_0, "neg": -0x10, "big": -18446744073709551617}`)
	if err != nil {
		t.Fatal(err)
	}
	okm := ok.(*node.OrderedMap)
	for k, want := range map[string]node.Node{"d": int64(1000), "h": int64(31), "o": int64(15), "b": int64(3), "neg": int64(-16)} {
		if v, _ := okm.Get(k); v != want {
			t.Errorf("%s = %v (%T), want %v", k, v, v, want)
		}
	}
	if v, _ := okm.Get("big"); v.(*big.Int).String() != "-18446744073709551617" {
		t.Errorf("big = %v", v)
	}

	// Empty containers and tuples.
	for _, input := range []string{`{}`, `[]`, `()`, `[(),{},[]]`} {
		if _, err := python.Parse(input); err != nil {
			t.Errorf("Parse(%q): %v", input, err)
		}
	}

	// The encoder escapes control characters as \xNN.
	out, err := python.Encode(nodetest.OM("k", "a\"b\\c\nd\re\tf"+string(rune(0))+string(rune(0x7f)), "empty", nodetest.Arr(), "m", nodetest.OM()))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`\"`, `\\`, `\n`, `\r`, `\t`, `\x00`, `\x7f`, "[]", "{}"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if _, err := python.Encode(nodetest.OM("m", nodetest.OM("n", math.NaN()))); !errors.Is(err, node.ErrUnsupportedStructure) {
		t.Error("nested NaN encoded")
	}
	if _, err := python.Encode(nodetest.Arr(math.Inf(-1))); !errors.Is(err, node.ErrUnsupportedStructure) {
		t.Error("-Inf in array encoded")
	}
}

func TestPythonRemainingBranches(t *testing.T) {
	for name, input := range map[string]string{
		"trailing data":      `{} junk`,
		"unexpected char":    `{"a": ?}`,
		"non-string key":     `{1: 2}`,
		"unterminated slash": `{"a": "x\`,
		"truncated hex":      `{"a": "\x`,
		"signed hex escape":  `{"a": "\x+1"}`,
		"bytes prefix":       `{"a": rub'x'}`,
		"prefix overrun":     `{"a": rubr'x'}`,
		"bare name":          `{"a": rubbish}`,
		"f-string":           `{"a": f"x"}`,
		"underscore in dot":  `{"a": 1._5}`,
	} {
		if _, err := python.Parse(input); err == nil {
			t.Errorf("%s: accepted %q", name, input)
		}
	}

	// Accepted forms: trailing commas, uppercase raw prefix, triple quotes with
	// an embedded quote, \0 followed by an octal digit, signed exponents,
	// negative floats and False.
	n, err := python.Parse(strings.Join([]string{
		`{"d": {"a": 1,},`,
		`"l": [1,],`,
		`"t": (1,),`,
		`"raw": R"a\nb",`,
		`"triple": '''a'b''',`,
		`"oct": "\01",`,
		`"exp": 1e+2,`,
		`"negexp": 1e-2,`,
		`"negf": -1.5,`,
		`"no": False}`,
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*node.OrderedMap)
	for k, want := range map[string]node.Node{
		"raw":    `a\nb`,
		"triple": "a'b",
		"oct":    string(rune(1)),
		"exp":    100.0,
		"negexp": 0.01,
		"negf":   -1.5,
		"no":     false,
	} {
		if v, _ := m.Get(k); v != want {
			t.Errorf("%s = %#v, want %#v", k, v, want)
		}
	}

	// False renders as a Python literal.
	if out, err := python.Encode(nodetest.OM("f", false)); err != nil || !strings.Contains(out, "False") {
		t.Errorf("Encode(false) = %q, %v", out, err)
	}
}
