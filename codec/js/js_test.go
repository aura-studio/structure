package js_test

import (
	"testing"

	"github.com/aura-studio/structure/v2/codec/js"
	"github.com/aura-studio/structure/v2/codec/json"
	"github.com/aura-studio/structure/v2/node"
)

func TestJavaScriptLiteralSubset(t *testing.T) {
	n, err := js.Parse(`({a: 1, "x-y": 'v', hex: 0x1f, oct: 0o17, big: 18446744073709551616n, __proto__: "safe", arr: [1,,3], a: 2})`)
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*node.OrderedMap)
	a, _ := m.Get("a")
	if a != int64(2) {
		t.Fatal(a)
	}
	p, _ := m.Get("__proto__")
	if p != "safe" {
		t.Fatal(p)
	}
	v, _ := m.Get("arr")
	if v.([]node.Node)[1] != nil {
		t.Fatal(v)
	}
	for _, bad := range []string{`1`, `({a})`, `({["a"]:1})`, `({get a(){return 1}})`, `({a:()=>1})`, `({a:function(){}})`, `({a:` + "`x`" + `})`, `({a:undefined})`, `({a:f()})`} {
		if _, err := js.Parse(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	out, err := js.Encode(n)
	if err != nil {
		t.Fatal(err)
	}
	// The JS encoder deliberately reuses the JSON renderer; pin that so a future
	// change cannot fork the two escapers silently.
	jsonOut, _ := json.Encode(n)
	if out != jsonOut {
		t.Fatal("JS output differs from JSON")
	}
}
