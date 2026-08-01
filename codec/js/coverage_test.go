package js_test

import (
	"testing"

	"github.com/aura-studio/structure/v2/codec/js"
	"github.com/aura-studio/structure/v2/node"
)

func TestJavaScriptEdges(t *testing.T) {
	n, err := js.Parse(`({1: "num", 0x2: "hex", plus: +1, minus: -2, f: -1.5, s: "A", nested: {a: [{b: 1}]}})`)
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*node.OrderedMap)
	for k, want := range map[string]node.Node{"1": "num", "2": "hex", "plus": int64(1), "minus": int64(-2), "f": -1.5, "s": "A"} {
		if v, _ := m.Get(k); v != want {
			t.Errorf("%s = %v (%T), want %v", k, v, v, want)
		}
	}
	for _, bad := range []string{
		`({a: -"x"})`, `({a: +"x"})`, `({a: b})`, `({a: 1, ...rest})`,
		`({1.5: "float key"})`, `({[Symbol()]: 1})`, `({a: new Date()})`,
		`({a: 1} , {b: 2})`, `"x"`, `1`, `null`, `true`, `({a: i++})`,
	} {
		if _, err := js.Parse(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	// Empty containers and nested elisions.
	if _, err := js.Parse(`({a: {}, b: [], c: [,,]})`); err != nil {
		t.Fatal(err)
	}
	if _, err := js.Parse(`([])`); err != nil {
		t.Fatal(err)
	}
}

func TestJSParserErrorPaths(t *testing.T) {
	for name, input := range map[string]string{
		"syntax":            `{`,
		"two expressions":   `1);(2`,
		"array element":     `({a: [b]})`,
		"nested unary":      `({a: -(-b)})`,
		"unary non-numeric": `({a: -"s"})`,
	} {
		if _, err := js.Parse(input); err == nil {
			t.Errorf("%s: accepted %q", name, input)
		}
	}
	// Reserved words are valid property keys (goja lowers them to string keys).
	n, err := js.Parse(`({null: 1, true: 2, class: 3})`)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"null", "true", "class"} {
		if _, ok := n.(*node.OrderedMap).Get(k); !ok {
			t.Errorf("missing key %q", k)
		}
	}
}
