package structure

import (
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"
)

func TestJSONParseAndEncode(t *testing.T) {
	input := `{"z":1,"a":[null,true,3.5,{"big":9007199254740993}],"u":18446744073709551615,"huge":18446744073709551616}`
	n, err := parseJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*OrderedMap)
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
	out, err := encodeJSON(n)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\n  \"a\": [") {
		t.Fatalf("not 2-space formatted:\n%s", out)
	}
	back, err := parseJSON(out)
	if err != nil || !nodeEqual(n, back, true) {
		t.Fatalf("round trip: %v", err)
	}
}

func TestJSONRootAndSyntaxErrors(t *testing.T) {
	if n, err := parseJSON(`[1,"x",false]`); err != nil || len(n.([]Node)) != 3 {
		t.Fatalf("array: %v %v", n, err)
	}
	for _, input := range []string{"", "1", "null", `{`, `{}` + ` {}`, `{"a":1} junk`} {
		if _, err := parseJSON(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	if _, err := parseJSON(`{"a":1,"a":2}`); !errors.Is(err, ErrDuplicateKey) {
		t.Fatalf("duplicate: %v", err)
	}
}

func TestJSONEncodeEdges(t *testing.T) {
	out, err := encodeJSON(om("whole", 7.0, "s", string(rune(0x2028)), "empty", arr()))
	if err != nil {
		t.Fatal(err)
	}
	escapedLineSeparator := string(rune(92)) + "u2028"
	if !strings.Contains(out, "7.0") || !strings.Contains(out, escapedLineSeparator) || !strings.Contains(out, "[]") {
		t.Fatal(out)
	}
	for _, bad := range []Node{om("n", math.NaN()), om("i", math.Inf(1))} {
		if _, err := encodeJSON(bad); !errors.Is(err, ErrUnsupportedStructure) {
			t.Errorf("err = %v", err)
		}
	}
}

func TestJSONDepth(t *testing.T) {
	if _, err := parseJSON(deepJSON(1000)); err != nil {
		t.Fatalf("1000 levels: %v", err)
	}
	if _, err := parseJSON(deepJSON(10001)); !errors.Is(err, ErrTooDeep) {
		t.Fatalf("10001 levels: %v", err)
	}
}
