package structure

import (
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"
)

func TestValidateRejectsIllegalTypes(t *testing.T) {
	bad := []Node{map[string]any{"a": 1}, []any{1, 2}, int(5), float32(1.5), []string{"a"}}
	for i, value := range bad {
		if err := validate(value); err == nil {
			t.Errorf("bad[%d] (%T) accepted", i, value)
		}
	}
	m := NewOrderedMap()
	m.Set("bad", (*big.Int)(nil))
	if err := validate(m); err == nil {
		t.Error("nil *big.Int accepted")
	}
	var nilMap *OrderedMap
	if err := validate(nilMap); err == nil {
		t.Error("nil *OrderedMap accepted")
	}
}

func TestValidateTopLevelScalar(t *testing.T) {
	for _, scalar := range []Node{int64(1), "x", true, 1.5, nil} {
		if err := validate(scalar); !errors.Is(err, ErrTopLevelScalar) {
			t.Errorf("validate(%v) err = %v, want ErrTopLevelScalar", scalar, err)
		}
	}
}

func TestValidateAcceptsAllLegalScalars(t *testing.T) {
	m := om("nil", nil, "bool", true, "int", int64(1), "uint", uint64(1<<63),
		"big", new(big.Int).Lsh(big.NewInt(1), 100), "float", 1.5, "str", "s",
		"arr", arr(nil, true, int64(1)))
	if err := validate(m); err != nil {
		t.Fatalf("valid tree rejected: %v", err)
	}
}

func TestValidateDepthBoundary(t *testing.T) {
	if err := validate(deepArrays(9999)); err != nil {
		t.Fatalf("9999-level tree rejected: %v", err)
	}
	if err := validate(deepArrays(10001)); !errors.Is(err, ErrTooDeep) {
		t.Fatalf("10001-level tree err = %v, want ErrTooDeep", err)
	}
}

func TestNumberNodeLadder(t *testing.T) {
	cases := []struct {
		in   string
		base int
		want Node
	}{
		{"42", 10, int64(42)}, {"-42", 10, int64(-42)},
		{"9223372036854775807", 10, int64(math.MaxInt64)},
		{"9223372036854775808", 10, uint64(math.MaxInt64 + 1)},
		{"18446744073709551615", 10, uint64(math.MaxUint64)},
		{"3.5", 10, 3.5}, {"1e3", 10, 1000.0},
		{"0x1F", 0, int64(31)}, {"0o17", 0, int64(15)}, {"0b101", 0, int64(5)},
	}
	for _, tc := range cases {
		got, err := numberNode(tc.in, tc.base)
		if err != nil || got != tc.want {
			t.Errorf("numberNode(%q) = %v (%T), %v; want %v (%T)", tc.in, got, got, err, tc.want, tc.want)
		}
	}
	got, err := numberNode("18446744073709551616", 10)
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := got.(*big.Int); !ok || b.String() != "18446744073709551616" {
		t.Fatalf("got %v (%T), want big.Int 2^64", got, got)
	}
	if _, err := numberNode("zzz", 10); err == nil {
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
		got, err := formatFloat(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("formatFloat(%v) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := formatFloat(bad); !errors.Is(err, ErrUnsupportedStructure) {
			t.Errorf("formatFloat(%v) err = %v", bad, err)
		}
	}
	if got, _ := formatNumber(big.NewInt(12)); got != "12" {
		t.Fatalf("formatNumber = %q", got)
	}
	if _, err := formatNumber("x"); err == nil {
		t.Fatal("nonnumeric value accepted")
	}
}

func TestJSONEscape(t *testing.T) {
	bs := string(rune(92))
	input := "a\"b\\c\nd\te" + string([]rune{0, 0x2028, 0x2029, 0xe9})
	got := jsonEscape(input)
	want := "\"a" + bs + "\"b" + bs + bs + "c" + bs + "n" + "d" + bs + "t" + "e" + bs + "u0000" + bs + "u2028" + bs + "u2029" + string(rune(0xe9)) + "\""
	if got != want {
		t.Fatalf("jsonEscape = %q, want %q", got, want)
	}
	if strings.ContainsRune(got, rune(0x2028)) || strings.ContainsRune(got, rune(0x2029)) {
		t.Fatal("raw U+2028/U+2029 present")
	}
}

func TestNodeCloneIndependent(t *testing.T) {
	b := big.NewInt(10)
	n := om("b", b, "l", arr(int64(1)))
	clone := nodeClone(n).(*OrderedMap)
	b.SetInt64(999)
	value, _ := clone.Get("b")
	if value.(*big.Int).Int64() != 10 {
		t.Fatal("big.Int not deep-copied")
	}
}
