package structure

import (
	"strings"
	"testing"
)

// om builds an OrderedMap from alternating key/value pairs.
func om(pairs ...any) *OrderedMap {
	m := NewOrderedMap()
	for i := 0; i+1 < len(pairs); i += 2 {
		m.Set(pairs[i].(string), pairs[i+1])
	}
	return m
}

// arr builds a []Node from values.
func arr(vals ...Node) []Node { return vals }

// fixtureRichMap is the shared nested fixture for matrix tests: strings,
// bools, integers, floats, nested maps and arrays of scalars/maps. No nils
// (TOML/Lua restrictions) and no NaN/Inf (YAML/TOML only).
func fixtureRichMap() Node {
	return om(
		"name", "structure",
		"count", int64(42),
		"big", int64(9007199254740993), // 2^53+1: precision witness
		"ratio", 3.5,
		"whole", 7.0, // integral float: must survive as float
		"active", true,
		"tags", arr("go", "convert"),
		"nested", om(
			"level", int64(2),
			"items", arr(
				om("id", int64(1), "label", "first"),
				om("id", int64(2), "label", "second"),
			),
		),
	)
}

// fixtureStringsMap uses only string values — the fixture for any path that
// goes through XML (whose values are untyped strings).
func fixtureStringsMap() Node {
	return om(
		"title", "hello world",
		"lang", "go",
		"inner", om(
			"note", "keep <this> & \"that\"",
			"list", arr("a", "b", "c"),
		),
	)
}

// fixtureArray is a top-level array fixture (JSON/YAML/Lua/Python/JS only).
func fixtureArray() Node {
	return arr(int64(1), "two", 3.5, true, om("k", "v"))
}

// deepArrays builds an n-level nested array: [[[...]]].
func deepArrays(n int) Node {
	var cur Node = int64(1)
	for i := 0; i < n; i++ {
		cur = arr(cur)
	}
	return cur
}

// deepMaps builds an n-level nested map: {a:{a:{...}}}.
func deepMaps(n int) Node {
	inner := om("leaf", int64(1))
	cur := Node(inner)
	for i := 0; i < n-1; i++ {
		cur = om("a", cur)
	}
	return cur
}

// deepString builds a nesting of n levels for parser depth tests per format.
func deepJSON(n int) string {
	return strings.Repeat("[", n) + "1" + strings.Repeat("]", n)
}

func deepYAML(n int) string {
	// Flow style nested sequences.
	return strings.Repeat("[", n) + "1" + strings.Repeat("]", n)
}

func deepLua(n int) string {
	return strings.Repeat("{", n) + "1" + strings.Repeat("}", n)
}

func deepPython(n int) string {
	return strings.Repeat("[", n) + "1" + strings.Repeat("]", n)
}

func deepJS(n int) string {
	return strings.Repeat("[", n) + "1" + strings.Repeat("]", n)
}

func deepTOML(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString("[")
		for j := 0; j <= i; j++ {
			if j > 0 {
				b.WriteString(".")
			}
			b.WriteString("a")
		}
		b.WriteString("]\n")
	}
	b.WriteString("x = 1\n")
	return b.String()
}

func deepXML(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString("<a>")
	}
	b.WriteString("x")
	for i := 0; i < n; i++ {
		b.WriteString("</a>")
	}
	return b.String()
}

// ordered reports whether format f preserves mapping key order semantically.
func ordered(f Format) bool {
	switch f {
	case JSON, YAML, Python, JS:
		return true
	}
	return false
}

// roundTrip encodes n to f, parses it back, and asserts deep equality.
func roundTrip(t *testing.T, f Format, n Node) {
	t.Helper()
	text, err := Encode(n, f)
	if err != nil {
		t.Fatalf("Encode(%s) error: %v", f, err)
	}
	back, err := Parse(text, f)
	if err != nil {
		t.Fatalf("Parse(%s) of encoded text error: %v\nencoded:\n%s", f, err, text)
	}
	if !nodeEqual(n, back, ordered(f)) {
		t.Fatalf("round-trip mismatch (%s):\noriginal: %#v\nencoded:\n%s\nparsed back: %#v", f, n, text, back)
	}
}
