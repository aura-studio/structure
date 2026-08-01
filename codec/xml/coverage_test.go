package xml

import (
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/aura-studio/structure/v2/internal/nodetest"
	"github.com/aura-studio/structure/v2/node"
)

// badNode is not a legal Node carrier; the writers must reject it. The
// "invalid node type" arms are unreachable from Encode (its validate() pass
// rejects the tree first), so they are driven directly.
var badNode node.Node = int(1)

func TestXMLRemainingBranches(t *testing.T) {
	// #text must be a scalar, in a plain element and inside an array.
	if _, err := Encode(nodetest.OM("root", nodetest.OM("#text", nodetest.Arr(int64(1))))); !errors.Is(err, node.ErrUnsupportedStructure) {
		t.Error("container #text encoded")
	}
	if _, err := Encode(nodetest.OM("root", nodetest.OM("list", nodetest.Arr(nodetest.OM("bad", math.NaN()))))); !errors.Is(err, node.ErrUnsupportedStructure) {
		t.Error("NaN inside a same-name array encoded")
	}
	if _, err := Encode(nodetest.OM("root", nodetest.OM("child", badNode))); err == nil {
		t.Error("illegal carrier encoded to XML")
	}
	if _, err := scalarString(nodetest.OM()); !errors.Is(err, node.ErrUnsupportedStructure) {
		t.Error("scalarString accepted a map")
	}
	if _, err := scalarString(nodetest.Arr()); !errors.Is(err, node.ErrUnsupportedStructure) {
		t.Error("scalarString accepted an array")
	}
	if _, err := scalarString(badNode); err == nil {
		t.Error("scalarString accepted an illegal carrier")
	}
}

func TestXMLValueCarriersAndDepth(t *testing.T) {
	// Encoding non-string scalars stringifies them; nil becomes an empty element.
	out, err := Encode(nodetest.OM("root", nodetest.OM(
		"@n", int64(1), "@f", 2.5, "@b", true, "@nil", nil,
		"i", int64(3), "u", uint64(4), "big", big.NewInt(5),
		"f", 6.5, "bool", false, "empty", nil,
		"list", nodetest.Arr("a", "b"),
	)))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`n="1"`, `f="2.5"`, `b="true"`, `nil=""`, "<i>3</i>", "<u>4</u>", "<big>5</big>", "<f>6.5</f>", "<bool>false</bool>", "<empty></empty>"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if _, err := Encode(nodetest.OM("root", nodetest.OM("bad", math.NaN()))); !errors.Is(err, node.ErrUnsupportedStructure) {
		t.Error("NaN encoded to XML")
	}
	if _, err := Encode(nodetest.OM("root", nodetest.OM("@bad", math.Inf(1)))); !errors.Is(err, node.ErrUnsupportedStructure) {
		t.Error("Inf attribute encoded")
	}
	if _, err := Encode(nodetest.OM()); !errors.Is(err, node.ErrUnsupportedStructure) {
		t.Error("root-less map encoded")
	}

	// Comments, processing instructions and doctypes are ignored; split text
	// segments concatenate.
	n, err := Parse(`<?xml version="1.0"?><!DOCTYPE r><r><!-- c -->a<?pi x?>b</r>`)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := n.(*node.OrderedMap).Get("r")
	if r != "ab" {
		t.Errorf("split text = %#v, want %q", r, "ab")
	}
	// An empty element parses to the empty string: an empty element is an absent
	// #text, and the encoder emits <r></r> for "", so this is what makes ""
	// round-trip.
	for _, input := range []string{`<r/>`, `<r></r>`, "<r> </r>"} {
		n, err := Parse(input)
		if err != nil {
			t.Fatal(err)
		}
		v, _ := n.(*node.OrderedMap).Get("r")
		if v != "" {
			t.Errorf("Parse(%q) element = %#v, want %q", input, v, "")
		}
	}
	// An element with an attribute keeps its map form.
	if n, err := Parse(`<r a="1"/>`); err != nil {
		t.Fatal(err)
	} else {
		v, _ := n.(*node.OrderedMap).Get("r")
		if m, ok := v.(*node.OrderedMap); !ok || m.Len() != 1 {
			t.Errorf("element with attribute = %#v", v)
		}
	}
	// The depth guard fires on deeply nested documents.
	if _, err := Parse(nodetest.DeepXML(100)); err != nil {
		t.Fatalf("100 levels: %v", err)
	}
	if _, err := Parse(nodetest.DeepXML(node.MaxDepth + 1)); !errors.Is(err, node.ErrTooDeep) {
		t.Errorf("%d levels err = %v, want ErrTooDeep", node.MaxDepth+1, err)
	}
	for _, input := range []string{`<a></b>`, `<a>&bad;</a>`, `</a>`, `<a attr></a>`} {
		if _, err := Parse(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

// encoding/xml writes malformed tags without complaining; the encoder has to
// reject names and characters XML 1.0 cannot represent.
func TestXMLEncoderRejectsMalformedOutput(t *testing.T) {
	for name, key := range map[string]string{
		"space in name":  "a b",
		"angle bracket":  "a>b",
		"leading digit":  "1a",
		"empty name":     "",
		"bare at":        "@",
		"quote in name":  `a"b`,
		"slash in name":  "a/b",
		"equals in name": "a=b",
		// A colon is a legal Name character, but the parser reads element names
		// as Name.Local, so <a:b> would come back as the key "b".
		"prefixed name": "a:b",
		"bare colon":    ":",
	} {
		if _, err := Encode(nodetest.OM("root", nodetest.OM(key, "x"))); !errors.Is(err, node.ErrUnsupportedStructure) {
			t.Errorf("%s (%q): err = %v, want ErrUnsupportedStructure", name, key, err)
		}
	}
	// Characters XML 1.0 forbids, in text and in attribute position.
	for _, bad := range []string{"x\x01y", "\x00", "x\x1fy", "￾"} {
		if _, err := Encode(nodetest.OM("root", nodetest.OM("a", bad))); !errors.Is(err, node.ErrUnsupportedStructure) {
			t.Errorf("text %q: err = %v, want ErrUnsupportedStructure", bad, err)
		}
		if _, err := Encode(nodetest.OM("root", nodetest.OM("@a", bad))); !errors.Is(err, node.ErrUnsupportedStructure) {
			t.Errorf("attribute %q: err = %v, want ErrUnsupportedStructure", bad, err)
		}
	}
	// Legal names and the whitespace XML does allow still encode, and re-parse.
	for _, tc := range []struct{ key, val string }{
		{"a", "x"}, {"_a", "x"}, {"a-b", "x"}, {"a.b", "x"}, {"a1", "x"},
		{"名前", "x"}, {"a", "tab\there"}, {"a", "nl\nhere"},
	} {
		src := nodetest.OM("root", nodetest.OM(tc.key, tc.val))
		out, err := Encode(src)
		if err != nil {
			t.Errorf("Encode(%q=%q): %v", tc.key, tc.val, err)
			continue
		}
		if _, err := Parse(out); err != nil {
			t.Errorf("encoded %q=%q does not re-parse: %v\n%s", tc.key, tc.val, err, out)
		}
	}
	// Namespace declarations keep their colon form and stay encodable.
	out, err := Encode(nodetest.OM("root", nodetest.OM("@xmlns", "urn:x", "@xmlns:p", "urn:y")))
	if err != nil {
		t.Fatalf("xmlns attributes rejected: %v", err)
	}
	if !strings.Contains(out, `xmlns="urn:x"`) || !strings.Contains(out, `xmlns:p="urn:y"`) {
		t.Errorf("xmlns output = %s", out)
	}
	if validAttrName("a:b:c") || validAttrName(":") {
		t.Error("validAttrName accepted a malformed qualified name")
	}
}

// An empty element is an absent #text, i.e. the empty string — that is what
// makes "" and nil round-trip through XML.
func TestXMLEmptyElementIsEmptyString(t *testing.T) {
	out, err := Encode(nodetest.OM("root", nodetest.OM("a", "")))
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if !node.Equal(back, nodetest.OM("root", nodetest.OM("a", "")), false) {
		t.Errorf(`empty string did not round-trip: %#v (encoded %q)`, back, out)
	}
}

// Indentation is whitespace inside the element, so for mixed content it used to
// be folded into #text on the way back in: {"#text":"hello"} re-parsed as
// {"#text":"hello\n"}. Such documents are now emitted unindented.
func TestXMLMixedContentRoundTrips(t *testing.T) {
	for _, want := range []*node.OrderedMap{
		nodetest.OM("root", nodetest.OM("#text", "hello", "child", "world")),
		nodetest.OM("root", nodetest.OM("child", nodetest.OM("#text", "a", "g", "b"))),
		nodetest.OM("root", nodetest.OM("@id", "7", "#text", "hello", "child", "world")),
		nodetest.OM("root", nodetest.OM("list", nodetest.Arr(nodetest.OM("#text", "x", "e", "y"), nodetest.OM("#text", "z", "e", "w")))),
	} {
		out, err := Encode(want)
		if err != nil {
			t.Errorf("encode %#v: %v", want, err)
			continue
		}
		got, err := Parse(out)
		if err != nil {
			t.Errorf("reparse %q: %v", out, err)
			continue
		}
		if !node.Equal(got, want, true) {
			t.Errorf("mixed content did not round-trip\n encoded %q\n got     %#v\n want    %#v", out, got, want)
		}
	}
	// Documents without mixed content stay indented.
	out, err := Encode(nodetest.OM("root", nodetest.OM("a", "1")))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(out, "\n  <a>") {
		t.Errorf("non-mixed document lost its indentation: %q", out)
	}
}
