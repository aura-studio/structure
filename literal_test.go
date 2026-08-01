package structure

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestLuaLiteralSubset(t *testing.T) {
	n, err := parseLua(`{ z = 1, ["a-b"] = "x\n", hex = 0x1f, long = [[ok]], gone = nil, z = 2 }`)
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*OrderedMap)
	z, _ := m.Get("z")
	if z != int64(2) {
		t.Fatal(z)
	}
	if _, ok := m.Get("gone"); ok {
		t.Fatal("nil key retained")
	}
	if a, err := parseLua(`{1, 2, 3,}`); err != nil || len(a.([]Node)) != 3 {
		t.Fatalf("%v %v", a, err)
	}
	for _, input := range []string{`1`, `{1,a=2}`, `{[1]=2}`, `function() end`, `f()`, `{nil}`} {
		if _, err := parseLua(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	out, err := encodeLua(om("while", "x", "a-b", "y", "ok", int64(1), "nil", nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `["while"]`) || !strings.Contains(out, `["a-b"]`) || strings.Contains(out, "nil =") {
		t.Fatal(out)
	}
	if _, err := encodeLua(arr(nil)); !errors.Is(err, ErrUnsupportedStructure) {
		t.Fatal(err)
	}
	if _, err := encodeLua(om("n", math.NaN())); !errors.Is(err, ErrUnsupportedStructure) {
		t.Fatal(err)
	}
}

func TestPythonLiteralSubset(t *testing.T) {
	input := `{"a": 0x1f, "o": 0o17, "b": 0b11, "big": 18446744073709551616, "f": 1_2.5, "s": "\x41B\U00000043\101", "raw": r"x\ny", "triple": '''a\nb''', "tuple": (1,2,)}`
	n, err := parsePython(input)
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*OrderedMap)
	for k, w := range map[string]Node{"a": int64(31), "o": int64(15), "b": int64(3), "f": 12.5, "s": "ABCA", "raw": `x\ny`, "triple": "a\nb"} {
		v, _ := m.Get(k)
		if v != w {
			t.Errorf("%s=%v (%T)", k, v, v)
		}
	}
	for _, bad := range []string{`1`, `{1,2}`, `{b"x":1}`, `{"a": f"x"}`, `{"a": foo()}`, `{"a": 01}`, `{"a": 1__2}`, `{"a": 1j}`, `{"a":"\N{x}"}`, `{"a":1,"a":2}`} {
		if _, err := parsePython(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	out, err := encodePython(n)
	if err != nil {
		t.Fatal(err)
	}
	if back, err := parsePython(out); err != nil || !nodeEqual(n, back, true) {
		t.Fatalf("round trip %v\n%s", err, out)
	}
	if _, err := encodePython(om("n", math.Inf(1))); !errors.Is(err, ErrUnsupportedStructure) {
		t.Fatal(err)
	}
}

func TestJavaScriptLiteralSubset(t *testing.T) {
	n, err := parseJS(`({a: 1, "x-y": 'v', hex: 0x1f, oct: 0o17, big: 18446744073709551616n, __proto__: "safe", arr: [1,,3], a: 2})`)
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*OrderedMap)
	a, _ := m.Get("a")
	if a != int64(2) {
		t.Fatal(a)
	}
	p, _ := m.Get("__proto__")
	if p != "safe" {
		t.Fatal(p)
	}
	v, _ := m.Get("arr")
	if v.([]Node)[1] != nil {
		t.Fatal(v)
	}
	for _, bad := range []string{`1`, `({a})`, `({["a"]:1})`, `({get a(){return 1}})`, `({a:()=>1})`, `({a:function(){}})`, `({a:` + "`x`" + `})`, `({a:undefined})`, `({a:f()})`} {
		if _, err := parseJS(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	out, err := encodeJS(n)
	if err != nil {
		t.Fatal(err)
	}
	jsonOut, _ := encodeJSON(n)
	if out != jsonOut {
		t.Fatal("JS output differs from JSON")
	}
}
