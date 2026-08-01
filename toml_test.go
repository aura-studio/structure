package structure

import (
	"errors"
	stderrors "errors"
	"math"
	"math/big"
	"strings"
	"testing"
)

func TestTOMLParseOrderAoTAndTime(t *testing.T) {
	input := "z = 1\na = 2\nd = 1979-05-27\nldt = 1979-05-27T07:32:00\nlt = 07:32:00\nodt = 1979-05-27T07:32:00Z\n[[items]]\nid = 1\n[[items]]\nid = 2\n"
	n, err := parseTOML(input)
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*OrderedMap)
	if got := strings.Join(m.Keys(), ","); got != "z,a,d,ldt,lt,odt,items" {
		t.Fatalf("order %s", got)
	}
	for k, want := range map[string]string{"d": "1979-05-27", "ldt": "1979-05-27T07:32:00", "lt": "07:32:00", "odt": "1979-05-27T07:32:00Z"} {
		v, _ := m.Get(k)
		if v != want {
			t.Errorf("%s=%v", k, v)
		}
	}
	v, _ := m.Get("items")
	if len(v.([]Node)) != 2 {
		t.Fatal(v)
	}
}

func TestTOMLEncodeAndRestrictions(t *testing.T) {
	n := om("title", "x\n", "whole", 7.0, "special", math.Inf(-1), "arr", arr(int64(1), int64(2)), "empty", om(), "items", arr(om("id", int64(1)), om("id", int64(2))))
	out, err := encodeTOML(n)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`title = "x\n"`, "whole = 7.0", "special = -inf", "[empty]", "[[items]]"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q:\n%s", s, out)
		}
	}
	if back, err := parseTOML(out); err != nil || !nodeEqual(n, back, false) {
		t.Fatalf("round trip: %v\n%s", err, out)
	}
	bad := []Node{arr(int64(1)), om("n", nil), om("u", uint64(math.MaxUint64)), om("b", new(big.Int).Lsh(big.NewInt(1), 80))}
	for _, v := range bad {
		if _, err := encodeTOML(v); !stderrors.Is(err, ErrUnsupportedStructure) || !stderrors.Is(err, errors.ErrUnsupported) {
			t.Errorf("%T err=%v", v, err)
		}
	}
}

func TestTOMLSyntaxError(t *testing.T) {
	if _, err := parseTOML("x = ["); err == nil {
		t.Fatal("invalid TOML accepted")
	}
}
