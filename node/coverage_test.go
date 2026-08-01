package node_test

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/aura-studio/structure/v2/internal/nodetest"
	"github.com/aura-studio/structure/v2/node"
)

// These tests target the scalar-carrier edges and error branches of the data
// model that the behavioural tests do not reach on their own.

func TestMarshalNodeAllCarriers(t *testing.T) {
	m := nodetest.OM(
		"nil", nil,
		"bool", false,
		"int", int64(-3),
		"uint", uint64(math.MaxUint64),
		"big", new(big.Int).Lsh(big.NewInt(1), 70),
		"float", 2.5,
		"whole", 4.0,
		"str", "s",
		"arr", nodetest.Arr(int64(1), nodetest.OM("k", "v"), nodetest.Arr()),
		"empty", nodetest.OM(),
	)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"nil":null`, `"bool":false`, `"int":-3`,
		`"uint":18446744073709551615`, `"big":1180591620717411303424`,
		`"float":2.5`, `"whole":4.0`, `"arr":[1,{"k":"v"},[]]`, `"empty":{}`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s in %s", want, b)
		}
	}
	// NaN has no JSON form, even through the Marshaler hook.
	if _, err := json.Marshal(nodetest.OM("n", math.NaN())); err == nil {
		t.Error("NaN marshalled")
	}
	if _, err := node.MarshalNode(int(1)); err == nil {
		t.Error("illegal node type marshalled")
	}
	var nilMap *node.OrderedMap
	if _, err := node.MarshalNode(nilMap); err == nil {
		t.Error("nil *OrderedMap marshalled")
	}
	// The rejection has to reach through the containers, not just the root.
	if _, err := node.MarshalNode(nodetest.Arr(int(1))); err == nil {
		t.Error("illegal array element marshalled")
	}
	if _, err := node.MarshalNode(nodetest.OM("k", int(1))); err == nil {
		t.Error("illegal map value marshalled")
	}
}

func TestNumberHelpersEdges(t *testing.T) {
	if !node.IsNumeric(uint64(1)) || !node.IsNumeric(big.NewInt(1)) || !node.IsNumeric(1.5) || !node.IsNumeric(int64(1)) {
		t.Error("IsNumeric rejected a numeric carrier")
	}
	if node.IsNumeric("1") || node.IsNumeric(nil) || node.IsNumeric(true) {
		t.Error("IsNumeric accepted a non-numeric value")
	}
	if got, err := node.FormatNumber(uint64(math.MaxUint64)); err != nil || got != "18446744073709551615" {
		t.Errorf("FormatNumber(uint64) = %q, %v", got, err)
	}
	if _, err := node.FormatNumber(math.NaN()); !errors.Is(err, node.ErrUnsupportedStructure) {
		t.Error("FormatNumber accepted NaN")
	}
	// A non-numeric carrier is an "invalid node" error, not the unsupported-shape
	// sentinel: the value is illegal in the model, not merely unrepresentable in
	// some target format.
	err := func() error { _, err := node.FormatNumber("x"); return err }()
	if err == nil || errors.Is(err, node.ErrUnsupportedStructure) {
		t.Errorf("FormatNumber(string) = %v, want a plain invalid-node error", err)
	} else if !strings.HasPrefix(err.Error(), node.MsgPrefix) {
		t.Errorf("FormatNumber(string) error %q lacks the module prefix", err)
	}
	if _, err := node.FormatNumber(nil); err == nil {
		t.Error("FormatNumber accepted nil")
	}
	if _, err := node.FormatNumber((*big.Int)(nil)); err == nil {
		t.Error("FormatNumber accepted a nil *big.Int")
	}

	// Negate covers every integer carrier plus the rejection path.
	for _, tc := range []struct {
		in, want node.Node
	}{
		{int64(1), int64(-1)},
		{1.5, -1.5},
		{uint64(1), int64(-1)},
		{big.NewInt(1), int64(-1)},
		{new(big.Int).Lsh(big.NewInt(1), 100), new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 100))},
	} {
		got, ok := node.Negate(tc.in)
		if !ok || !node.Equal(got, tc.want, true) {
			t.Errorf("Negate(%v) = %v, %v; want %v", tc.in, got, ok, tc.want)
		}
	}
	for _, bad := range []node.Node{"x", nil, true} {
		if _, ok := node.Negate(bad); ok {
			t.Errorf("Negate accepted %#v", bad)
		}
	}

	// BigResult narrows to the smallest exact carrier.
	if got := node.BigResult(big.NewInt(7)); got != int64(7) {
		t.Errorf("BigResult(7) = %v (%T)", got, got)
	}
	if got := node.BigResult(new(big.Int).SetUint64(math.MaxUint64)); got != uint64(math.MaxUint64) {
		t.Errorf("BigResult(2^64-1) = %v (%T)", got, got)
	}
	huge := new(big.Int).Lsh(big.NewInt(1), 100)
	if got := node.BigResult(huge); got != huge {
		t.Errorf("BigResult(2^100) = %v (%T)", got, got)
	}

	// AsBigInt accepts the integer carriers and refuses everything else.
	for _, tc := range []struct {
		in   node.Node
		want string
	}{
		{int64(-3), "-3"},
		{uint64(math.MaxUint64), "18446744073709551615"},
		{big.NewInt(5), "5"},
	} {
		got, ok := node.AsBigInt(tc.in)
		if !ok || got.String() != tc.want {
			t.Errorf("AsBigInt(%v) = %v, %v; want %s", tc.in, got, ok, tc.want)
		}
	}
	for _, bad := range []node.Node{1.5, "1", nil, true} {
		if _, ok := node.AsBigInt(bad); ok {
			t.Errorf("AsBigInt accepted %#v", bad)
		}
	}
}

func TestCloneAndEqualEdges(t *testing.T) {
	// Clone covers every carrier, including nested containers.
	src := nodetest.OM("a", nodetest.Arr(int64(1), "s", nil, true, 1.5, uint64(2), big.NewInt(3)), "m", nodetest.OM("x", int64(1)))
	clone := node.Clone(src).(*node.OrderedMap)
	if !node.Equal(src, clone, true) {
		t.Fatal("clone not equal to source")
	}
	cloneArr, _ := clone.Get("a")
	cloneArr.([]node.Node)[0] = int64(99)
	srcArr, _ := src.Get("a")
	if srcArr.([]node.Node)[0] != int64(1) {
		t.Fatal("clone shares the array with the source")
	}

	// Cross-carrier numeric comparisons and mismatched container shapes.
	cases := []struct {
		a, b node.Node
		want bool
	}{
		{uint64(math.MaxUint64), new(big.Int).SetUint64(math.MaxUint64), true},
		{big.NewInt(-1), int64(-1), true},
		{uint64(1), 1.0, false},
		{nodetest.Arr(int64(1)), nodetest.Arr(int64(1), int64(2)), false},
		{nodetest.OM("a", int64(1)), nodetest.OM("a", int64(1), "b", int64(2)), false},
		{nodetest.OM("a", int64(1)), nodetest.OM("b", int64(1)), false},
		{"x", int64(1), false},
		{true, int64(1), false},
	}
	for i, c := range cases {
		if got := node.Equal(c.a, c.b, true); got != c.want {
			t.Errorf("case %d: Equal(%v, %v) = %v, want %v", i, c.a, c.b, got, c.want)
		}
	}
	if node.Equal(nodetest.OM("a", int64(1)), nodetest.OM("b", int64(1)), false) {
		t.Error("unordered compare ignored key names")
	}
}

func TestCloneAndEqualDefensiveArms(t *testing.T) {
	// Clone passes typed nil pointers through untouched rather than dereferencing.
	if got := node.Clone((*node.OrderedMap)(nil)); got.(*node.OrderedMap) != nil {
		t.Error("nil *OrderedMap cloned into something")
	}
	if got := node.Clone((*big.Int)(nil)); got.(*big.Int) != nil {
		t.Error("nil *big.Int cloned into something")
	}
	// Equal: carrier mismatches, nested inequality, illegal types. An illegal
	// carrier is never equal to anything, including itself — Equal is only
	// defined over the legal model.
	var badNode node.Node = int(1)
	for i, c := range []struct {
		a, b node.Node
		want bool
	}{
		{1.5, "x", false},
		{nodetest.OM("a", nodetest.OM("b", int64(1))), nodetest.OM("a", nodetest.OM("b", int64(2))), false},
		{badNode, badNode, false},
		{(*big.Int)(nil), int64(0), false},
	} {
		if got := node.Equal(c.a, c.b, true); got != c.want {
			t.Errorf("case %d: Equal = %v, want %v", i, got, c.want)
		}
	}
	// JSONEscape covers the whole escape table.
	got := node.JSONEscape("a\rb\bc\fd")
	for _, want := range []string{`\r`, `\b`, `\f`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}

func TestOrderedMapRemainingBranches(t *testing.T) {
	m := node.NewOrderedMap()
	if _, ok := m.Get("missing"); ok {
		t.Error("Get on empty map reported a hit")
	}
	if m.Delete("missing") {
		t.Error("Delete on empty map reported success")
	}
	if len(m.Keys()) != 0 || m.Len() != 0 {
		t.Error("empty map is not empty")
	}
	// Deleting the only entry empties both the index and the list.
	m.Set("only", int64(1))
	if !m.Delete("only") || m.Len() != 0 || len(m.Keys()) != 0 {
		t.Error("deleting the sole entry left state behind")
	}
	// Re-adding after emptying rebuilds the list head/tail.
	m.Set("a", int64(1))
	m.Set("b", int64(2))
	if got := strings.Join(m.Keys(), ","); got != "a,b" {
		t.Errorf("keys after rebuild = %s", got)
	}
	// Clone of an empty map is independent.
	empty := node.NewOrderedMap()
	clone := empty.Clone()
	clone.Set("x", int64(1))
	if empty.Len() != 0 {
		t.Error("clone wrote through to the source")
	}
	// All() over an empty map yields nothing.
	for range empty.All() {
		t.Error("All() yielded an entry for an empty map")
	}
	// A zero-value OrderedMap is usable without NewOrderedMap: Set builds the
	// index lazily, so an embedded or composite-literal value still works.
	var zero node.OrderedMap
	zero.Set("a", int64(1))
	if v, ok := zero.Get("a"); !ok || v != int64(1) {
		t.Errorf("zero-value Set/Get = %v, %v", v, ok)
	}
}
