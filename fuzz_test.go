package structure

import (
	"strings"
	"testing"
)

// fuzzFormats indexes the seven formats for the fuzz selector byte.
var fuzzFormats = []Format{JSON, XML, YAML, TOML, Lua, Python, JS}

// fuzzInputLimit bounds generated inputs. Third-party parsers (goja, golua,
// yaml.v3) recurse before our depth guard can fire, and a genuine stack
// overflow is not recoverable — so pathological lengths are skipped rather
// than reported as findings.
const fuzzInputLimit = 4096

// FuzzParse asserts the parser contract on arbitrary input: never panic, and
// on success always return a mapping or array root (never a bare scalar).
func FuzzParse(f *testing.F) {
	seeds := []struct {
		sel  uint8
		text string
	}{
		{0, `{"a":1,"b":[true,null,"x"]}`},
		{0, `[1,2,{"k":"v"}]`},
		{0, `{"big":18446744073709551616,"f":1.5}`},
		{1, `<r a="1"><c>x</c><c>y</c></r>`},
		{1, `<?xml version="1.0"?><root><n x="y">t</n></root>`},
		{1, `<a><![CDATA[<raw>]]></a>`},
		{2, "a: 1\nb:\n  - x\n  - y\n"},
		{2, "- 1\n- two\n"},
		{2, "n: .nan\ni: .inf\nt: 2026-08-01T00:00:00Z\n"},
		{3, "a = 1\nb = \"x\"\n[t]\nc = true\n"},
		{3, "[[items]]\nid = 1\n[[items]]\nid = 2\n"},
		{3, "d = 1979-05-27\nlt = 07:32:00\n"},
		{4, `{ a = 1, b = "x", c = { d = true } }`},
		{4, `{1, 2, 3}`},
		{4, `{ hex = 0x1f, neg = -2.5, long = [[raw]] }`},
		{5, `{"a": 1, "b": (1, 2), "c": None}`},
		{5, `[1, True, "x", {"k": 0o17}]`},
		{5, `{"s": '''triple''', "r": r"x\ny"}`},
		{6, `({a: 1, "b-c": [1,,3], d: null})`},
		{6, `([1, 2, {e: 0x1f}])`},
		{6, `({big: 18446744073709551616n, neg: -1.5})`},
	}
	for _, s := range seeds {
		f.Add(s.sel, []byte(s.text))
	}

	f.Fuzz(func(t *testing.T, sel uint8, data []byte) {
		if len(data) > fuzzInputLimit {
			t.Skip("input beyond the documented length bound")
		}
		format := fuzzFormats[int(sel)%len(fuzzFormats)]
		node, err := Parse(string(data), format)
		if err != nil {
			if node != nil {
				t.Fatalf("Parse(%s) returned both a node and an error %v", format, err)
			}
			return
		}
		switch node.(type) {
		case *OrderedMap, []Node:
		default:
			t.Fatalf("Parse(%s) accepted a non-container root %T for %q", format, node, data)
		}
		// A parsed tree must satisfy the Node model invariants.
		if err := validate(node); err != nil {
			t.Fatalf("Parse(%s) produced an invalid tree: %v", format, err)
		}
	})
}

// FuzzRoundTrip asserts that whatever a parser accepts can be re-encoded and
// re-parsed to an equal tree, for every target the capability matrix allows.
func FuzzRoundTrip(f *testing.F) {
	f.Add(uint8(0), []byte(`{"a":1,"b":["x",true]}`))
	f.Add(uint8(2), []byte("a: 1\nb: [x, true]\n"))
	f.Add(uint8(3), []byte("a = 1\n[[t]]\nid = 1\n"))
	f.Add(uint8(5), []byte(`{"a": 1, "b": [None, True]}`))
	// Regression: a stray continuation byte used to survive the byte-oriented
	// parsers and then become U+FFFD in the rune-ranging encoders, silently
	// corrupting the round trip. Both boundaries now reject invalid UTF-8.
	stray := string([]byte{0x84})
	f.Add(uint8(5), []byte(`{"`+stray+`":[]}`))
	f.Add(uint8(4), []byte(`{ a = "`+stray+`" }`))

	f.Fuzz(func(t *testing.T, sel uint8, data []byte) {
		if len(data) > fuzzInputLimit {
			t.Skip("input beyond the documented length bound")
		}
		format := fuzzFormats[int(sel)%len(fuzzFormats)]
		node, err := Parse(string(data), format)
		if err != nil {
			return
		}
		text, err := Encode(node, format)
		if err != nil {
			// Only capability limits may reject a freshly parsed tree.
			if strings.Contains(err.Error(), "unsupported") {
				return
			}
			t.Fatalf("Encode(%s) rejected a parsed tree: %v", format, err)
		}
		back, err := Parse(text, format)
		if err != nil {
			t.Fatalf("re-parsing encoded %s failed: %v\n%s", format, err, text)
		}
		if !nodeEqual(node, back, ordered(format)) {
			t.Fatalf("round-trip mismatch (%s)\ninput: %q\nencoded:\n%s", format, data, text)
		}
	})
}
