package structure

import (
	"errors"
	stderrors "errors"
	"strings"
	"testing"
)

func TestXMLConventionsAndRoundTrip(t *testing.T) {
	input := `<?xml version="1.0"?><root id="7"><item>A</item><item><![CDATA[B&<]]></item><node x="y"> text </node></root>`
	n, err := parseXML(input)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := n.(*OrderedMap).Get("root")
	rm := root.(*OrderedMap)
	id, _ := rm.Get("@id")
	if id != "7" {
		t.Fatal(id)
	}
	items, _ := rm.Get("item")
	a := items.([]Node)
	if len(a) != 2 || a[1] != "B&<" {
		t.Fatal(a)
	}
	out, err := encodeXML(n)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, xmlHeaderForTest) || !strings.Contains(out, "&amp;") {
		t.Fatal(out)
	}
	back, err := parseXML(out)
	if err != nil || !nodeEqual(n, back, true) {
		t.Fatalf("round trip: %v\n%s", err, out)
	}
}

const xmlHeaderForTest = `<?xml version="1.0" encoding="UTF-8"?>`

func TestXMLNamespacesAndErrors(t *testing.T) {
	n, err := parseXML(`<r xmlns="u" xmlns:p="v" p:a="1"><p:x>v</p:x></r>`)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := n.(*OrderedMap).Get("r")
	m := r.(*OrderedMap)
	for _, k := range []string{"@xmlns", "@xmlns:p", "@a", "x"} {
		if _, ok := m.Get(k); !ok {
			t.Errorf("missing %s: %v", k, m.Keys())
		}
	}
	for _, input := range []string{"", "text", "<a/><b/>", "<a>", "x<a/>"} {
		if _, err := parseXML(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	for _, bad := range []Node{arr("x"), om("a", "x", "b", "y"), om("root", arr("x"))} {
		if _, err := encodeXML(bad); !stderrors.Is(err, ErrUnsupportedStructure) || !stderrors.Is(err, errors.ErrUnsupported) {
			t.Errorf("err=%v", err)
		}
	}
}

func TestXMLHelpers(t *testing.T) {
	if line, col := xmlLineCol("a\nb", 2); line != 2 || col != 1 {
		t.Fatalf("%d,%d", line, col)
	}
	p := om()
	xmlAddChild(p, "x", "a")
	xmlAddChild(p, "x", "b")
	xmlAddChild(p, "x", "c")
	v, _ := p.Get("x")
	if len(v.([]Node)) != 3 {
		t.Fatal(v)
	}
}
