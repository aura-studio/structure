// Package nodetest holds the node fixtures and deep-nesting generators shared
// by the tests of every package in this module.
//
// It lives under internal/ at the module root so all packages here can import
// it and nothing outside the module can. It deliberately depends on [node]
// alone: importing [format] or the root facade would make it unusable from
// those packages' own in-package tests, which is exactly where several of these
// fixtures are needed.
//
// Anything that needs a Format (round-tripping through the facade, the
// key-order predicate) stays in the test file that needs it; for order use
// format.PreservesOrder rather than a copy here.
package nodetest

import (
	"strings"

	"github.com/aura-studio/structure/v2/node"
)

// OM builds an OrderedMap from alternating key/value pairs. Keys must be
// strings; a non-string key panics, which in a test is the desired report.
func OM(pairs ...any) *node.OrderedMap {
	m := node.NewOrderedMap()
	for i := 0; i+1 < len(pairs); i += 2 {
		m.Set(pairs[i].(string), pairs[i+1])
	}
	return m
}

// Arr builds a []node.Node from values.
func Arr(vals ...node.Node) []node.Node { return vals }

// RichMap is the shared nested fixture for matrix tests: strings, bools,
// integers, floats, nested maps and arrays of scalars/maps. No nils (TOML/Lua
// restrictions) and no NaN/Inf (YAML/TOML only).
func RichMap() node.Node {
	return OM(
		"name", "structure",
		"count", int64(42),
		"big", int64(9007199254740993), // 2^53+1: precision witness
		"ratio", 3.5,
		"whole", 7.0, // integral float: must survive as float
		"active", true,
		"tags", Arr("go", "convert"),
		"nested", OM(
			"level", int64(2),
			"items", Arr(
				OM("id", int64(1), "label", "first"),
				OM("id", int64(2), "label", "second"),
			),
		),
	)
}

// Array is a top-level array fixture (JSON/YAML/Lua/Python/JS only).
func Array() node.Node {
	return Arr(int64(1), "two", 3.5, true, OM("k", "v"))
}

// DeepArrays builds an n-level nested array: [[[...]]].
func DeepArrays(n int) node.Node {
	var cur node.Node = int64(1)
	for i := 0; i < n; i++ {
		cur = Arr(cur)
	}
	return cur
}

// DeepMaps builds an n-level nested map: {a:{a:{...}}}.
func DeepMaps(n int) node.Node {
	inner := OM("leaf", int64(1))
	cur := node.Node(inner)
	for i := 0; i < n-1; i++ {
		cur = OM("a", cur)
	}
	return cur
}

// DeepJSON builds n levels of nested JSON arrays.
func DeepJSON(n int) string {
	return strings.Repeat("[", n) + "1" + strings.Repeat("]", n)
}

// DeepYAML builds n levels of nested YAML flow sequences.
func DeepYAML(n int) string {
	return strings.Repeat("[", n) + "1" + strings.Repeat("]", n)
}

// DeepLua builds n levels of nested Lua table constructors.
func DeepLua(n int) string {
	return strings.Repeat("{", n) + "1" + strings.Repeat("}", n)
}

// DeepPython builds n levels of nested Python lists.
func DeepPython(n int) string {
	return strings.Repeat("[", n) + "1" + strings.Repeat("]", n)
}

// DeepJS builds n levels of nested JavaScript arrays.
func DeepJS(n int) string {
	return strings.Repeat("[", n) + "1" + strings.Repeat("]", n)
}

// DeepTOML builds n levels of nested TOML tables.
func DeepTOML(n int) string {
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
