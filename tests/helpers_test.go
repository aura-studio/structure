package tests

import (
	"testing"

	structure "github.com/aura-studio/structure/v2"
	"github.com/aura-studio/structure/v2/format"
	"github.com/aura-studio/structure/v2/internal/nodetest"
	"github.com/aura-studio/structure/v2/node"
)

// The fixtures themselves live in internal/nodetest so that the codec packages'
// own tests can share them. What follows are one-line forwarders under the names
// the root tests already used, plus the two helpers that genuinely belong here
// because they need the facade's format-dispatching Parse/Encode.

// These tests live in their own package, one directory down, so they reach the
// facade the way any caller does: through its exported API only. The forwarders
// below re-bind that API to the bare names the assertions already used, which
// keeps the move a move — no assertion changed shape just to gain a qualifier.
// Anything NOT reachable this way is deliberately absent: the facade's
// unexported firstInvalidUTF8 is now exercised through Parse, which is the only
// way a caller can reach it anyway.
type (
	Node       = structure.Node
	OrderedMap = structure.OrderedMap
	Format     = structure.Format
	ParseError = structure.ParseError
)

const (
	JSON   = structure.JSON
	YAML   = structure.YAML
	TOML   = structure.TOML
	Lua    = structure.Lua
	Python = structure.Python
	JS     = structure.JS
)

var (
	ErrUnsupportedStructure = structure.ErrUnsupportedStructure
	ErrTooDeep              = structure.ErrTooDeep
	ErrTopLevelScalar       = structure.ErrTopLevelScalar
	ErrDuplicateKey         = structure.ErrDuplicateKey
)

func Parse(input string, f Format) (Node, error) { return structure.Parse(input, f) }
func Encode(n Node, f Format) (string, error)    { return structure.Encode(n, f) }
func Convert(in string, from, to Format) (string, error) {
	return structure.Convert(in, from, to)
}
func ParseFormat(s string) (Format, error) { return structure.ParseFormat(s) }
func NewOrderedMap() *OrderedMap           { return structure.NewOrderedMap() }

// maxDepth mirrors node.MaxDepth for the tests that probe the limit.
const maxDepth = node.MaxDepth

func om(pairs ...any) *OrderedMap { return nodetest.OM(pairs...) }
func arr(vals ...Node) []Node     { return nodetest.Arr(vals...) }

func fixtureRichMap() Node { return nodetest.RichMap() }
func fixtureArray() Node   { return nodetest.Array() }

func deepArrays(n int) Node { return nodetest.DeepArrays(n) }
func deepMaps(n int) Node   { return nodetest.DeepMaps(n) }

func deepJSON(n int) string   { return nodetest.DeepJSON(n) }
func deepYAML(n int) string   { return nodetest.DeepYAML(n) }
func deepLua(n int) string    { return nodetest.DeepLua(n) }
func deepPython(n int) string { return nodetest.DeepPython(n) }
func deepJS(n int) string     { return nodetest.DeepJS(n) }
func deepTOML(n int) string   { return nodetest.DeepTOML(n) }

// nodeEqual forwards to node.Equal, the module's single deep-equality rule.
func nodeEqual(a, b Node, keyOrder bool) bool { return node.Equal(a, b, keyOrder) }

// validate forwards to node.Validate. The facade's own tests assert the model
// invariant on trees Parse returns, which is a facade-level contract even though
// the rule itself lives in package node.
func validate(n Node) error { return node.Validate(n) }

// ordered reports whether format f preserves mapping key order semantically.
func ordered(f Format) bool { return format.PreservesOrder(f) }

// allFormats lists every supported format, for tests that sweep all six.
var allFormats = format.All()

// roundTrip encodes n to f, parses it back, and asserts deep equality. It stays
// at the root because it goes through the facade's format-dispatching API.
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
