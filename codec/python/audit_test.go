package python_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/aura-studio/structure/v2/codec/python"
	"github.com/aura-studio/structure/v2/format"
	"github.com/aura-studio/structure/v2/internal/nodetest"
	"github.com/aura-studio/structure/v2/node"
)

// Regressions for defects found by the adversarial audit.

// A duplicate key used to surface as a bare sentinel with no position. It now
// carries the offending location while staying reachable via errors.Is.
func TestPythonDuplicateKeyCarriesPosition(t *testing.T) {
	var pe *format.ParseError
	err := func() error { _, err := python.Parse("{\"a\": 1,\n\"a\": 2}"); return err }()
	if !errors.Is(err, format.ErrDuplicateKey) {
		t.Fatalf("not ErrDuplicateKey: %v", err)
	}
	if !errors.As(err, &pe) {
		t.Fatalf("err is %T, want *format.ParseError wrapping the sentinel", err)
	}
	if pe.Line != 2 || pe.Column < 1 {
		t.Errorf("position = line %d col %d, want line 2 col >= 1", pe.Line, pe.Column)
	}
	if !strings.Contains(pe.Error(), "duplicate") {
		t.Errorf("error text %q lacks 'duplicate'", pe.Error())
	}
}

// A Go string is UTF-8, so an escape naming a surrogate or a value past
// U+10FFFF has no representation; those used to become U+FFFD silently.
func TestPythonRejectsUnrepresentableEscapes(t *testing.T) {
	for name, input := range map[string]string{
		`\U surrogate low`:  `{"k": "\U0000D800"}`,
		`\U surrogate high`: `{"k": "\U0000DFFF"}`,
		`\U above max`:      `{"k": "\U00110000"}`,
		`\U far above max`:  `{"k": "\UFFFFFFFF"}`,
		`\u surrogate`:      `{"k": "\uD800"}`,
		`\u surrogate low`:  `{"k": "\uDFFF"}`,
		`in key`:            `{"\uD800": 1}`,
	} {
		if _, err := python.Parse(input); err == nil {
			t.Errorf("%s: accepted %s", name, input)
		}
	}
	// Everything inside the representable range still decodes exactly.
	for input, want := range map[string]string{
		`{"k": "\U00000041"}`: "A",
		`{"k": "\U0010FFFF"}`: "\U0010FFFF",
		`{"k": "\U0001F600"}`: "\U0001F600",
		`{"k": "￿"}`:          "￿",
		`{"k": "퟿"}`:          "퟿",
		`{"k": ""}`:           "",
		`{"k": "\xff"}`:       "ÿ",
	} {
		n, err := python.Parse(input)
		if err != nil {
			t.Errorf("%s: %v", input, err)
			continue
		}
		if v, _ := n.(*node.OrderedMap).Get("k"); v != want {
			t.Errorf("%s -> %q, want %q", input, v, want)
		}
	}
}

// ast.literal_eval("(1)") is the scalar 1: parentheses group, only a comma
// builds a tuple. The parser used to wrap every parenthesized value in a
// one-element array.
func TestPythonParenthesesAreNotAlwaysTuples(t *testing.T) {
	for input, want := range map[string]node.Node{
		`{"k": (1)}`:         int64(1),
		`{"k": ("s")}`:       "s",
		`{"k": (True)}`:      true,
		`{"k": (None)}`:      nil,
		`{"k": (-1.5)}`:      -1.5,
		`{"k": ((1))}`:       int64(1),
		`{"k": (1,)}`:        nodetest.Arr(int64(1)),
		`{"k": (1, 2)}`:      nodetest.Arr(int64(1), int64(2)),
		`{"k": (1, 2,)}`:     nodetest.Arr(int64(1), int64(2)),
		`{"k": ()}`:          nodetest.Arr(),
		`{"k": ([1])}`:       nodetest.Arr(int64(1)),
		`{"k": ((1,),)}`:     nodetest.Arr(nodetest.Arr(int64(1))),
		`{"k": (1, (2, 3))}`: nodetest.Arr(int64(1), nodetest.Arr(int64(2), int64(3))),
	} {
		n, err := python.Parse(input)
		if err != nil {
			t.Errorf("%s: %v", input, err)
			continue
		}
		got, _ := n.(*node.OrderedMap).Get("k")
		if !node.Equal(got, want, true) {
			t.Errorf("%s -> %T %#v, want %T %#v", input, got, got, want, want)
		}
	}
	// A parenthesized scalar at the root is a scalar, so it is still rejected.
	if _, err := python.Parse(`(1)`); !errors.Is(err, node.ErrTopLevelScalar) {
		t.Errorf("root (1): err = %v, want ErrTopLevelScalar", err)
	}
	// ...but a real one-element tuple is a valid root.
	if n, err := python.Parse(`(1,)`); err != nil {
		t.Errorf("root (1,): %v", err)
	} else if !node.Equal(n, nodetest.Arr(int64(1)), true) {
		t.Errorf("root (1,) -> %#v", n)
	}
}
