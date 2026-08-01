package python_test

import (
	"errors"
	"math"
	"testing"

	"github.com/aura-studio/structure/v2/codec/python"
	"github.com/aura-studio/structure/v2/internal/nodetest"
	"github.com/aura-studio/structure/v2/node"
)

func TestPythonLiteralSubset(t *testing.T) {
	input := `{"a": 0x1f, "o": 0o17, "b": 0b11, "big": 18446744073709551616, "f": 1_2.5, "s": "\x41B\U00000043\101", "raw": r"x\ny", "triple": '''a\nb''', "tuple": (1,2,)}`
	n, err := python.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*node.OrderedMap)
	for k, w := range map[string]node.Node{"a": int64(31), "o": int64(15), "b": int64(3), "f": 12.5, "s": "ABCA", "raw": `x\ny`, "triple": "a\nb"} {
		v, _ := m.Get(k)
		if v != w {
			t.Errorf("%s=%v (%T)", k, v, v)
		}
	}
	for _, bad := range []string{`1`, `{1,2}`, `{b"x":1}`, `{"a": f"x"}`, `{"a": foo()}`, `{"a": 01}`, `{"a": 1__2}`, `{"a": 1j}`, `{"a":"\N{x}"}`, `{"a":1,"a":2}`} {
		if _, err := python.Parse(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	out, err := python.Encode(n)
	if err != nil {
		t.Fatal(err)
	}
	if back, err := python.Parse(out); err != nil || !node.Equal(n, back, true) {
		t.Fatalf("round trip %v\n%s", err, out)
	}
	if _, err := python.Encode(nodetest.OM("n", math.Inf(1))); !errors.Is(err, node.ErrUnsupportedStructure) {
		t.Fatal(err)
	}
}
