// These tests are in-package (package xml, not xml_test) because addChild is
// unexported. Importing internal/nodetest from here is safe: nodetest depends on
// package node alone, so there is no cycle back to this package.
package xml

import (
	"errors"
	"strings"
	"testing"

	"github.com/aura-studio/structure/v2/internal/nodetest"
	"github.com/aura-studio/structure/v2/node"
)

const xmlHeaderForTest = `<?xml version="1.0" encoding="UTF-8"?>`

func TestXMLConventionsAndRoundTrip(t *testing.T) {
	input := `<?xml version="1.0"?><root id="7"><item>A</item><item><![CDATA[B&<]]></item><node x="y"> text </node></root>`
	n, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := n.(*node.OrderedMap).Get("root")
	rm := root.(*node.OrderedMap)
	id, _ := rm.Get("@id")
	if id != "7" {
		t.Fatal(id)
	}
	items, _ := rm.Get("item")
	a := items.([]node.Node)
	if len(a) != 2 || a[1] != "B&<" {
		t.Fatal(a)
	}
	out, err := Encode(n)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, xmlHeaderForTest) || !strings.Contains(out, "&amp;") {
		t.Fatal(out)
	}
	back, err := Parse(out)
	if err != nil || !node.Equal(n, back, true) {
		t.Fatalf("round trip: %v\n%s", err, out)
	}
}

func TestXMLNamespacesAndErrors(t *testing.T) {
	n, err := Parse(`<r xmlns="u" xmlns:p="v" p:a="1"><p:x>v</p:x></r>`)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := n.(*node.OrderedMap).Get("r")
	m := r.(*node.OrderedMap)
	for _, k := range []string{"@xmlns", "@xmlns:p", "@a", "x"} {
		if _, ok := m.Get(k); !ok {
			t.Errorf("missing %s: %v", k, m.Keys())
		}
	}
	for _, input := range []string{"", "text", "<a/><b/>", "<a>", "x<a/>"} {
		if _, err := Parse(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	for _, bad := range []node.Node{
		nodetest.Arr("x"),
		nodetest.OM("a", "x", "b", "y"),
		nodetest.OM("root", nodetest.Arr("x")),
	} {
		if _, err := Encode(bad); !errors.Is(err, node.ErrUnsupportedStructure) || !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("err=%v", err)
		}
	}
}

// TestXMLAddChild covers the same-name-sibling merge: the first occurrence is
// stored bare, the second turns the slot into an array, and later ones append.
func TestXMLAddChild(t *testing.T) {
	p := nodetest.OM()
	addChild(p, "x", "a")
	addChild(p, "x", "b")
	addChild(p, "x", "c")
	v, _ := p.Get("x")
	if len(v.([]node.Node)) != 3 {
		t.Fatal(v)
	}
}
