package json_test

import (
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/aura-studio/structure/v2/codec/json"
	"github.com/aura-studio/structure/v2/format"
	"github.com/aura-studio/structure/v2/internal/nodetest"
	"github.com/aura-studio/structure/v2/node"
)

func TestJSONParseAndEncode(t *testing.T) {
	input := `{"z":1,"a":[null,true,3.5,{"big":9007199254740993}],"u":18446744073709551615,"huge":18446744073709551616}`
	n, err := json.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*node.OrderedMap)
	if got := m.Keys(); strings.Join(got, ",") != "z,a,u,huge" {
		t.Fatalf("order = %v", got)
	}
	u, _ := m.Get("u")
	if u != uint64(math.MaxUint64) {
		t.Fatalf("u = %v (%T)", u, u)
	}
	h, _ := m.Get("huge")
	if h.(*big.Int).String() != "18446744073709551616" {
		t.Fatal(h)
	}
	out, err := json.Encode(n)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\n  \"a\": [") {
		t.Fatalf("not 2-space formatted:\n%s", out)
	}
	back, err := json.Parse(out)
	if err != nil || !node.Equal(n, back, true) {
		t.Fatalf("round trip: %v", err)
	}
}

func TestJSONRootAndSyntaxErrors(t *testing.T) {
	if n, err := json.Parse(`[1,"x",false]`); err != nil || len(n.([]node.Node)) != 3 {
		t.Fatalf("array: %v %v", n, err)
	}
	for _, input := range []string{"", "1", "null", `{`, `{}` + ` {}`, `{"a":1} junk`} {
		if _, err := json.Parse(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	if _, err := json.Parse(`{"a":1,"a":2}`); !errors.Is(err, format.ErrDuplicateKey) {
		t.Fatalf("duplicate: %v", err)
	}
}

func TestJSONEncodeEdges(t *testing.T) {
	out, err := json.Encode(nodetest.OM("whole", 7.0, "s", string(rune(0x2028)), "empty", nodetest.Arr()))
	if err != nil {
		t.Fatal(err)
	}
	escapedLineSeparator := string(rune(92)) + "u2028"
	if !strings.Contains(out, "7.0") || !strings.Contains(out, escapedLineSeparator) || !strings.Contains(out, "[]") {
		t.Fatal(out)
	}
	for _, bad := range []node.Node{nodetest.OM("n", math.NaN()), nodetest.OM("i", math.Inf(1))} {
		if _, err := json.Encode(bad); !errors.Is(err, node.ErrUnsupportedStructure) {
			t.Errorf("err = %v", err)
		}
	}
}

func TestJSONDepth(t *testing.T) {
	if _, err := json.Parse(nodetest.DeepJSON(1000)); err != nil {
		t.Fatalf("1000 levels: %v", err)
	}
	if _, err := json.Parse(nodetest.DeepJSON(node.MaxDepth + 1)); !errors.Is(err, node.ErrTooDeep) {
		t.Fatalf("%d levels: %v", node.MaxDepth+1, err)
	}
}
