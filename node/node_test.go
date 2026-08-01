// The node tests live in an external test package (package node_test) rather
// than in package node. Everything they exercise is exported, and the external
// package can import internal/nodetest for the shared fixtures — an in-package
// test file could not, because nodetest imports node.
package node_test

import (
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/aura-studio/structure/v2/internal/nodetest"
	"github.com/aura-studio/structure/v2/node"
)

func TestValidateRejectsIllegalTypes(t *testing.T) {
	bad := []node.Node{map[string]any{"a": 1}, []any{1, 2}, int(5), float32(1.5), []string{"a"}}
	for i, value := range bad {
		if err := node.Validate(value); err == nil {
			t.Errorf("bad[%d] (%T) accepted", i, value)
		}
	}
	m := node.NewOrderedMap()
	m.Set("bad", (*big.Int)(nil))
	if err := node.Validate(m); err == nil {
		t.Error("nil *big.Int accepted")
	}
	var nilMap *node.OrderedMap
	if err := node.Validate(nilMap); err == nil {
		t.Error("nil *OrderedMap accepted")
	}
}

func TestValidateTopLevelScalar(t *testing.T) {
	for _, scalar := range []node.Node{int64(1), "x", true, 1.5, nil} {
		if err := node.Validate(scalar); !errors.Is(err, node.ErrTopLevelScalar) {
			t.Errorf("Validate(%v) err = %v, want ErrTopLevelScalar", scalar, err)
		}
	}
}

func TestValidateAcceptsAllLegalScalars(t *testing.T) {
	m := nodetest.OM("nil", nil, "bool", true, "int", int64(1), "uint", uint64(1<<63),
		"big", new(big.Int).Lsh(big.NewInt(1), 100), "float", 1.5, "str", "s",
		"arr", nodetest.Arr(nil, true, int64(1)))
	if err := node.Validate(m); err != nil {
		t.Fatalf("valid tree rejected: %v", err)
	}
}

func TestValidateDepthBoundary(t *testing.T) {
	if err := node.Validate(nodetest.DeepArrays(node.MaxDepth - 1)); err != nil {
		t.Fatalf("%d-level tree rejected: %v", node.MaxDepth-1, err)
	}
	if err := node.Validate(nodetest.DeepArrays(node.MaxDepth + 1)); !errors.Is(err, node.ErrTooDeep) {
		t.Fatalf("%d-level tree err = %v, want ErrTooDeep", node.MaxDepth+1, err)
	}
}

func TestNumberNodeLadder(t *testing.T) {
	cases := []struct {
		in   string
		base int
		want node.Node
	}{
		{"42", 10, int64(42)}, {"-42", 10, int64(-42)},
		{"9223372036854775807", 10, int64(math.MaxInt64)},
		{"9223372036854775808", 10, uint64(math.MaxInt64 + 1)},
		{"18446744073709551615", 10, uint64(math.MaxUint64)},
		{"3.5", 10, 3.5}, {"1e3", 10, 1000.0},
		{"0x1F", 0, int64(31)}, {"0o17", 0, int64(15)}, {"0b101", 0, int64(5)},
	}
	for _, tc := range cases {
		got, err := node.NumberNode(tc.in, tc.base)
		if err != nil || got != tc.want {
			t.Errorf("NumberNode(%q) = %v (%T), %v; want %v (%T)", tc.in, got, got, err, tc.want, tc.want)
		}
	}
	got, err := node.NumberNode("18446744073709551616", 10)
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := got.(*big.Int); !ok || b.String() != "18446744073709551616" {
		t.Fatalf("got %v (%T), want big.Int 2^64", got, got)
	}
	if _, err := node.NumberNode("zzz", 10); err == nil {
		t.Error("invalid number accepted")
	}
}

func TestFormatFloatAndNumber(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{3, "3.0"}, {-2, "-2.0"}, {0, "0.0"}, {1.5, "1.5"}, {0.1, "0.1"},
		{1e21, "1e+21"}, {5e-324, "5e-324"},
	}
	for _, tc := range cases {
		got, err := node.FormatFloat(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("FormatFloat(%v) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := node.FormatFloat(bad); !errors.Is(err, node.ErrUnsupportedStructure) {
			t.Errorf("FormatFloat(%v) err = %v", bad, err)
		}
	}
	if got, _ := node.FormatNumber(big.NewInt(12)); got != "12" {
		t.Fatalf("FormatNumber = %q", got)
	}
	if _, err := node.FormatNumber("x"); err == nil {
		t.Fatal("nonnumeric value accepted")
	}
}

func TestJSONEscape(t *testing.T) {
	bs := string(rune(92))
	input := "a\"b\\c\nd\te" + string([]rune{0, 0x2028, 0x2029, 0xe9})
	got := node.JSONEscape(input)
	want := "\"a" + bs + "\"b" + bs + bs + "c" + bs + "n" + "d" + bs + "t" + "e" + bs + "u0000" + bs + "u2028" + bs + "u2029" + string(rune(0xe9)) + "\""
	if got != want {
		t.Fatalf("JSONEscape = %q, want %q", got, want)
	}
	if strings.ContainsRune(got, rune(0x2028)) || strings.ContainsRune(got, rune(0x2029)) {
		t.Fatal("raw U+2028/U+2029 present")
	}
}

func TestNodeCloneIndependent(t *testing.T) {
	b := big.NewInt(10)
	n := nodetest.OM("b", b, "l", nodetest.Arr(int64(1)))
	clone := node.Clone(n).(*node.OrderedMap)
	b.SetInt64(999)
	value, _ := clone.Get("b")
	if value.(*big.Int).Int64() != 10 {
		t.Fatal("big.Int not deep-copied")
	}
}
