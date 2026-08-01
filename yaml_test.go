package structure

import (
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"
)

func TestYAMLScalarsOrderAndRoundTrip(t *testing.T) {
	input := "z: 1\na: True\nnope: false\nyes_word: yes\ntime: 2026-08-01T12:30:00Z\nbig: 18446744073709551616\nfloat: 7.0\nnan: .nan\ninf: .inf\n"
	n, err := parseYAML(input)
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*OrderedMap)
	if strings.Join(m.Keys(), ",") != "z,a,nope,yes_word,time,big,float,nan,inf" {
		t.Fatal(m.Keys())
	}
	v, _ := m.Get("a")
	if v != true {
		t.Fatalf("True parsed as %v", v)
	}
	v, _ = m.Get("yes_word")
	if v != "yes" {
		t.Fatalf("yes = %v (%T)", v, v)
	}
	v, _ = m.Get("time")
	if v != "2026-08-01T12:30:00Z" {
		t.Fatal(v)
	}
	v, _ = m.Get("big")
	if v.(*big.Int).String() != "18446744073709551616" {
		t.Fatal(v)
	}
	v, _ = m.Get("nan")
	if !math.IsNaN(v.(float64)) {
		t.Fatal(v)
	}
	out, err := encodeYAML(n)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "big: !!int 18446744073709551616") {
		t.Fatal(out)
	}
}

func TestYAMLErrors(t *testing.T) {
	cases := []string{"", "x", "---\na: 1\n---\nb: 2\n", "a: &x 1\nb: *x\n", "a: !!binary YQ==\n", "!custom {a: 1}\n", "a: 1\na: 2\n", "base: &b {x: 1}\nout: {<<: *b}\n"}
	for _, input := range cases {
		if _, err := parseYAML(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	if _, err := parseYAML("a: 1\na: 2\n"); !errors.Is(err, ErrDuplicateKey) {
		t.Fatalf("duplicate = %v", err)
	}
}

func TestYAMLArrayAndSpecialEncode(t *testing.T) {
	if n, err := parseYAML("- 1\n- two\n"); err != nil || len(n.([]Node)) != 2 {
		t.Fatalf("%v %v", n, err)
	}
	out, err := encodeYAML(om("nan", math.NaN(), "pos", math.Inf(1), "neg", math.Inf(-1), "nested", om("x", int64(1))))
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{".nan", ".inf", "-.inf", "  x: 1"} {
		if !strings.Contains(out, text) {
			t.Errorf("missing %q in %s", text, out)
		}
	}
}
