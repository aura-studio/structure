package node_test

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/aura-studio/structure/v2/internal/nodetest"
	"github.com/aura-studio/structure/v2/node"
)

// TestFromAnyCarrierMapping walks the whole accepted type list. Each case
// asserts the CARRIER, not just the value: a uint landing on int64 or an int
// landing on float64 would still compare equal numerically while breaking the
// ladder every encoder depends on.
func TestFromAnyCarrierMapping(t *testing.T) {
	// uint's width is platform-dependent, so its expected carrier is derived
	// rather than hardcoded: 64-bit uint(MaxUint) overflows int64 and must ride
	// uint64, while a 32-bit one fits int64.
	maxUint := uint(math.MaxUint)
	wantMaxUint := node.Node(uint64(maxUint))
	if uint64(maxUint) <= math.MaxInt64 {
		wantMaxUint = int64(maxUint)
	}

	for _, tc := range []struct {
		name string
		in   any
		want node.Node
	}{
		{"nil", nil, nil},
		{"true", true, true},
		{"false", false, false},
		{"string", "x", "x"},
		{"empty string", "", ""},

		{"int", int(7), int64(7)},
		{"int negative", int(-7), int64(-7)},
		{"int8", int8(-8), int64(-8)},
		{"int16", int16(-16), int64(-16)},
		{"int32", int32(-32), int64(-32)},
		{"int64", int64(-64), int64(-64)},
		{"int64 min", int64(math.MinInt64), int64(math.MinInt64)},

		{"uint small", uint(8), int64(8)},
		{"uint max", maxUint, wantMaxUint},
		{"uint8 max", uint8(math.MaxUint8), int64(math.MaxUint8)},
		{"uint16 max", uint16(math.MaxUint16), int64(math.MaxUint16)},
		{"uint32 max", uint32(math.MaxUint32), int64(math.MaxUint32)},
		{"uint64 fits int64", uint64(9), int64(9)},
		{"uint64 max int64", uint64(math.MaxInt64), int64(math.MaxInt64)},
		{"uint64 past int64", uint64(math.MaxInt64) + 1, uint64(math.MaxInt64) + 1},
		{"uint64 max", uint64(math.MaxUint64), uint64(math.MaxUint64)},

		{"float64", 1.5, 1.5},
		{"float64 integral", float64(2), float64(2)},
		{"float32", float32(1.5), float64(1.5)},

		{"big.Int small", big.NewInt(5), int64(5)},
		{"big.Int past uint64", new(big.Int).Lsh(big.NewInt(1), 100), new(big.Int).Lsh(big.NewInt(1), 100)},

		{"json.Number int", json.Number("42"), int64(42)},
		{"json.Number float", json.Number("1.5"), 1.5},
		{"json.Number past uint64", json.Number("18446744073709551616"), new(big.Int).Lsh(big.NewInt(1), 64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := node.FromAny(tc.in)
			if err != nil {
				t.Fatalf("FromAny(%#v): %v", tc.in, err)
			}
			if reflect.TypeOf(got) != reflect.TypeOf(tc.want) {
				t.Fatalf("FromAny(%#v) carrier = %T, want %T", tc.in, got, tc.want)
			}
			if !node.Equal(got, tc.want, true) {
				t.Fatalf("FromAny(%#v) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}

// A uint past MaxInt64 must never take the int64 branch: int64(uint64(1<<63))
// is silently negative, which is the one numeric bug in this conversion that a
// value-only comparison would not catch.
func TestFromAnyUnsignedNeverGoesNegative(t *testing.T) {
	for _, in := range []any{uint64(math.MaxUint64), uint64(math.MaxInt64) + 1} {
		got, err := node.FromAny(in)
		if err != nil {
			t.Fatalf("FromAny(%v): %v", in, err)
		}
		u, ok := got.(uint64)
		if !ok {
			t.Fatalf("FromAny(%v) carrier = %T, want uint64", in, got)
		}
		if u != in.(uint64) {
			t.Errorf("FromAny(%v) = %v, value changed", in, u)
		}
	}
}

// json.Number is the one input type whose text can be malformed. A named string
// type must not fall through to "case string" either, which would turn a number
// into a quoted string.
func TestFromAnyJSONNumber(t *testing.T) {
	if _, err := node.FromAny(json.Number("not-a-number")); err == nil {
		t.Error("FromAny accepted a malformed json.Number")
	}
	// Full precision beyond float64's 2^53: the reason UseNumber exists.
	got, err := node.FromAny(json.Number("9007199254740993"))
	if err != nil {
		t.Fatalf("FromAny: %v", err)
	}
	if got != int64(9007199254740993) {
		t.Errorf("json.Number lost precision: got %#v", got)
	}
}

// The input tree stays untouched: a *big.Int is copied, and a []any is rebuilt
// rather than converted in place. In-place writes are easy to reach for here
// because Node is an alias for any, which makes []any and []Node the same type
// and the mutation legal to write.
func TestFromAnyDoesNotMutateInput(t *testing.T) {
	// The value has to stay past uint64 so it keeps riding *big.Int: a small one
	// normalizes onto int64, and then the int64 conversion — not the copy — is
	// what protects it, so the assertion would prove nothing.
	want := new(big.Int).Lsh(big.NewInt(1), 100)
	b := new(big.Int).Set(want)
	got, err := node.FromAny(map[string]any{"n": b})
	if err != nil {
		t.Fatalf("FromAny: %v", err)
	}
	b.SetInt64(9)
	v, _ := got.(*node.OrderedMap).Get("n")
	if bi, ok := v.(*big.Int); !ok {
		t.Errorf("carrier = %T, want *big.Int", v)
	} else if bi.Cmp(want) != 0 {
		t.Errorf("value = %v, want %v: input *big.Int was aliased", bi, want)
	}

	// []any holding native maps: the slice must not be rewritten under the
	// caller, who still owns it.
	inner := map[string]any{"a": 1}
	in := []any{inner, 2}
	if _, err := node.FromAny(in); err != nil {
		t.Fatalf("FromAny: %v", err)
	}
	if _, ok := in[0].(map[string]any); !ok {
		t.Errorf("input slice element rewritten to %T", in[0])
	}
	if in[1] != 2 {
		t.Errorf("input slice element 1 = %#v, want int 2", in[1])
	}
	if _, ok := inner["a"].(int); !ok {
		t.Errorf("input map value rewritten to %T", inner["a"])
	}
}

// Go maps have no order, so FromAny imposes one. It has to be sorted rather
// than whatever range yields: five of the six encoders emit *OrderedMap in
// insertion order, so a bare range would make their output differ between runs
// of the same binary.
func TestFromAnyMapKeysAreSorted(t *testing.T) {
	in := map[string]any{"delta": 4, "alpha": 1, "Zulu": 0, "charlie": 3, "bravo": 2, "": -1}
	want := make([]string, 0, len(in))
	for k := range in {
		want = append(want, k)
	}
	sort.Strings(want)

	// Repeated to catch a range-order dependency: Go randomizes map iteration,
	// so an unsorted implementation fails this within a few attempts.
	for i := 0; i < 50; i++ {
		got, err := node.FromAny(in)
		if err != nil {
			t.Fatalf("FromAny: %v", err)
		}
		if keys := got.(*node.OrderedMap).Keys(); !reflect.DeepEqual(keys, want) {
			t.Fatalf("attempt %d: keys = %q, want %q", i, keys, want)
		}
	}
}

// The default mode sorts every mapping, an *OrderedMap in the input included.
// Canonicalization is the whole point: equivalent data must convert to identical
// text whatever its source, and a mapping that arrived already ordered would
// otherwise leak that order into the output.
func TestFromAnyDefaultSortsEvenOrderedMaps(t *testing.T) {
	in := nodetest.OM("zebra", int64(1), "apple", int64(2), "mango", int64(3))
	got, err := node.FromAny(in)
	if err != nil {
		t.Fatalf("FromAny: %v", err)
	}
	want := []string{"apple", "mango", "zebra"}
	if keys := got.(*node.OrderedMap).Keys(); !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %q, want %q (sorted, not insertion order)", keys, want)
	}
	// Sorting reads the input's keys but must not reorder it: Keys hands back a
	// fresh slice, and sorting that slice cannot reach the caller's map.
	if keys := in.Keys(); !reflect.DeepEqual(keys, []string{"zebra", "apple", "mango"}) {
		t.Errorf("input *OrderedMap reordered to %q", keys)
	}
}

// KeepOrder is the other half of the trade: a mapping that already carries an
// order keeps it, while a Go map — which has none to keep — is still sorted. The
// option preserves order, it does not invent one.
func TestFromAnyKeepOrderPreservesOrderedMapButSortsGoMaps(t *testing.T) {
	in := nodetest.OM("zebra", int64(1), "apple", int64(2), "mango", int64(3))
	got, err := node.FromAny(in, node.KeepOrder())
	if err != nil {
		t.Fatalf("FromAny: %v", err)
	}
	want := []string{"zebra", "apple", "mango"}
	if keys := got.(*node.OrderedMap).Keys(); !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %q, want %q (insertion order, not sorted)", keys, want)
	}
	// A Go map nested inside an ordered one still sorts, so the two rules apply
	// per level rather than per call.
	in2 := nodetest.OM("m", map[string]any{"b": 1, "a": 2})
	got2, err := node.FromAny(in2, node.KeepOrder())
	if err != nil {
		t.Fatalf("FromAny: %v", err)
	}
	nested, _ := got2.(*node.OrderedMap).Get("m")
	m, ok := nested.(*node.OrderedMap)
	if !ok {
		t.Fatalf("nested value = %T, want *node.OrderedMap", nested)
	}
	if keys := m.Keys(); !reflect.DeepEqual(keys, []string{"a", "b"}) {
		t.Errorf("nested Go map keys = %q, want sorted even under KeepOrder", keys)
	}
}

// Two guarantees are mode-independent, so they are asserted in both: the result
// never aliases the input, and a native container nested inside an *OrderedMap is
// converted rather than carried through as an illegal carrier.
func TestFromAnyOrderedMapInvariantsHoldInBothModes(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []node.Option
	}{
		{"default", nil},
		{"keeporder", []node.Option{node.KeepOrder()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := nodetest.OM("zebra", int64(1), "apple", int64(2))
			got, err := node.FromAny(in, tc.opts...)
			if err != nil {
				t.Fatalf("FromAny: %v", err)
			}
			// A fresh map, not the input: nothing the caller holds is shared.
			if got == node.Node(in) {
				t.Error("FromAny returned the input *OrderedMap instead of a copy")
			}

			in2 := nodetest.OM("m", map[string]any{"b": 1, "a": 2})
			got2, err := node.FromAny(in2, tc.opts...)
			if err != nil {
				t.Fatalf("FromAny: %v", err)
			}
			nested, _ := got2.(*node.OrderedMap).Get("m")
			m, ok := nested.(*node.OrderedMap)
			if !ok {
				t.Fatalf("nested value = %T, want *node.OrderedMap", nested)
			}
			if keys := m.Keys(); !reflect.DeepEqual(keys, []string{"a", "b"}) {
				t.Errorf("nested keys = %q, want sorted", keys)
			}
		})
	}
}

// jsonOf renders a Node as JSON. It is the most compact whole-tree,
// order-sensitive expectation available: OrderedMap.MarshalJSON emits insertion
// order, so one string pins every level's key sequence at once — which is exactly
// what the mode axis changes.
func jsonOf(t *testing.T, n node.Node) string {
	t.Helper()
	b, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return string(b)
}

// The mode is one axis and the input's shape is the other, so the contract is a
// matrix rather than a pair of examples. Two rules cover every cell: a Go map
// sorts in both modes because it has no order to keep, and an *OrderedMap sorts
// by default but keeps its own order under KeepOrder. They apply per level, so a
// mixed tree exercises both at once.
func TestFromAnyModeStructureMatrix(t *testing.T) {
	for _, tc := range []struct {
		name        string
		in          any
		wantDefault string
		wantKeep    string
	}{
		{
			name:        "go map",
			in:          map[string]any{"z": int64(1), "a": int64(2)},
			wantDefault: `{"a":2,"z":1}`,
			wantKeep:    `{"a":2,"z":1}`,
		},
		{
			name:        "ordered map",
			in:          nodetest.OM("z", int64(1), "a", int64(2)),
			wantDefault: `{"a":2,"z":1}`,
			wantKeep:    `{"z":1,"a":2}`,
		},
		{
			name:        "go map holding ordered map",
			in:          map[string]any{"m": nodetest.OM("z", int64(1), "a", int64(2)), "k": int64(0)},
			wantDefault: `{"k":0,"m":{"a":2,"z":1}}`,
			wantKeep:    `{"k":0,"m":{"z":1,"a":2}}`,
		},
		{
			name:        "ordered map holding go map",
			in:          nodetest.OM("m", map[string]any{"z": int64(1), "a": int64(2)}, "k", int64(0)),
			wantDefault: `{"k":0,"m":{"a":2,"z":1}}`,
			wantKeep:    `{"m":{"a":2,"z":1},"k":0}`,
		},
		{
			name: "array of mappings",
			in: []any{
				nodetest.OM("z", int64(1), "a", int64(2)),
				map[string]any{"y": int64(3), "b": int64(4)},
			},
			wantDefault: `[{"a":2,"z":1},{"b":4,"y":3}]`,
			wantKeep:    `[{"z":1,"a":2},{"b":4,"y":3}]`,
		},
		{
			name:        "empty mappings",
			in:          map[string]any{"om": nodetest.OM(), "gm": map[string]any{}},
			wantDefault: `{"gm":{},"om":{}}`,
			wantKeep:    `{"gm":{},"om":{}}`,
		},
		{
			// "" sorts before everything, so listing it last is what makes the two
			// modes distinguishable here.
			name:        "empty string key",
			in:          nodetest.OM("a", int64(1), "", int64(0)),
			wantDefault: `{"":0,"a":1}`,
			wantKeep:    `{"a":1,"":0}`,
		},
		{
			name: "deep nesting",
			in: nodetest.OM(
				"z", nodetest.OM("y", nodetest.OM("x", int64(1), "w", int64(2)), "v", int64(3)),
				"u", int64(4),
			),
			wantDefault: `{"u":4,"z":{"v":3,"y":{"w":2,"x":1}}}`,
			wantKeep:    `{"z":{"y":{"x":1,"w":2},"v":3},"u":4}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotDefault, err := node.FromAny(tc.in)
			if err != nil {
				t.Fatalf("FromAny: %v", err)
			}
			if got := jsonOf(t, gotDefault); got != tc.wantDefault {
				t.Errorf("default mode = %s, want %s", got, tc.wantDefault)
			}

			gotKeep, err := node.FromAny(tc.in, node.KeepOrder())
			if err != nil {
				t.Fatalf("FromAny(KeepOrder): %v", err)
			}
			if got := jsonOf(t, gotKeep); got != tc.wantKeep {
				t.Errorf("KeepOrder mode = %s, want %s", got, tc.wantKeep)
			}
		})
	}
}

// Both modes must be deterministic, which is the point of the feature: KeepOrder
// relaxes canonicalization for mappings that carry an order, not for Go maps. The
// repetition is what catches a bare range — Go randomizes map iteration, so an
// implementation that forgot to sort fails within a few attempts.
func TestFromAnyKeyOrderIsDeterministicInBothModes(t *testing.T) {
	in := map[string]any{
		"delta": int64(4),
		"alpha": int64(1),
		"cfg":   nodetest.OM("zebra", int64(1), "apple", int64(2), "mango", int64(3)),
	}
	const (
		wantDefault = `{"alpha":1,"cfg":{"apple":2,"mango":3,"zebra":1},"delta":4}`
		wantKeep    = `{"alpha":1,"cfg":{"zebra":1,"apple":2,"mango":3},"delta":4}`
	)

	for i := 0; i < 50; i++ {
		gotDefault, err := node.FromAny(in)
		if err != nil {
			t.Fatalf("attempt %d: FromAny: %v", i, err)
		}
		if got := jsonOf(t, gotDefault); got != wantDefault {
			t.Fatalf("attempt %d: default mode = %s, want %s", i, got, wantDefault)
		}

		gotKeep, err := node.FromAny(in, node.KeepOrder())
		if err != nil {
			t.Fatalf("attempt %d: FromAny(KeepOrder): %v", i, err)
		}
		if got := jsonOf(t, gotKeep); got != wantKeep {
			t.Fatalf("attempt %d: KeepOrder mode = %s, want %s", i, got, wantKeep)
		}
	}
}

// A stray Stringer must NOT be accepted. *big.Int, json.Number, time.Time,
// big.Float, time.Duration and net.IP all implement String(), so a Stringer
// fallback would turn 1<<100 into a quoted string that then passes Validate:
// wrong, plausible-looking, and silent. Same for a named type aliasing a legal
// carrier — accepting it would mean guessing a representation the caller never
// chose.
func TestFromAnyRejectsUnsupportedTypes(t *testing.T) {
	type namedString string
	type namedInt int
	type point struct{ X, Y int }
	var nilPtr *point
	ch := make(chan int)

	for _, in := range []any{
		// Typed maps and slices: only map[string]any and []any are the shapes
		// encoding/json produces, and they are what the feature is for.
		map[any]any{"a": 1},
		map[string]string{"a": "b"},
		map[string]int{"a": 1},
		map[int]any{1: "a"},
		[]string{"a"},
		[]int{1},
		[]byte("bytes"),
		[2]int{1, 2},
		// Structs and pointers: no field-name convention is this package's to pick.
		point{1, 2},
		&point{1, 2},
		nilPtr,
		new(int),
		// Stringers whose String() would be a lie about the value's type.
		time.Now(),
		time.Second,
		big.NewFloat(1.5),
		stringerString("x"),
		// Named types over legal carriers still need an explicit conversion.
		namedString("x"),
		namedInt(1),
		// Types with no text form at all.
		ch,
		func() {},
		complex(1, 2),
		complex64(complex(1, 2)),
		uintptr(1),
		unsafe.Pointer(nil),
		error(errors.New("boom")),
	} {
		if _, err := node.FromAny(in); err == nil {
			t.Errorf("FromAny(%T) was accepted", in)
		} else if !strings.HasPrefix(err.Error(), node.MsgPrefix) {
			t.Errorf("FromAny(%T) error %q lacks the module prefix", in, err)
		}
	}
}

// stringerString is a Stringer whose String() looks like a perfectly good
// conversion, which is exactly why the converter must not have a Stringer
// fallback: the same fallback would swallow *big.Int and json.Number.
type stringerString string

func (s stringerString) String() string { return string(s) }

// A rejection deep in the tree surfaces rather than being dropped or replaced
// with a zero value, in both container kinds.
func TestFromAnyPropagatesNestedErrors(t *testing.T) {
	for name, in := range map[string]any{
		"in map":         map[string]any{"ok": 1, "bad": []string{"x"}},
		"in slice":       []any{1, []string{"x"}},
		"under ordered":  nodetest.OM("bad", []string{"x"}),
		"deep":           map[string]any{"a": []any{map[string]any{"b": make(chan int)}}},
		"nil big.Int":    map[string]any{"b": (*big.Int)(nil)},
		"nil OrderedMap": map[string]any{"m": (*node.OrderedMap)(nil)},
	} {
		if _, err := node.FromAny(in); err == nil {
			t.Errorf("%s: FromAny accepted the tree", name)
		}
	}
}

// A typed-nil *OrderedMap is rejected rather than carried through. node.Equal
// calls av.Len() without a nil guard, so a Node holding one panics on
// comparison — this converter must never be the thing that produces it.
func TestFromAnyRejectsTypedNilOrderedMap(t *testing.T) {
	if _, err := node.FromAny((*node.OrderedMap)(nil)); err == nil {
		t.Error("FromAny accepted a nil *OrderedMap at the root")
	}
	if _, err := node.FromAny((*big.Int)(nil)); err == nil {
		t.Error("FromAny accepted a nil *big.Int at the root")
	}
}

// The depth guard is the first statement of the recursion because Go's stack
// overflow is fatal and cannot be recovered. Before FromAny existed, a
// 200k-deep map[string]any was harmless only because Validate's root check does
// not recognize map[string]any as a root at all.
func TestFromAnyDepthGuard(t *testing.T) {
	deepMap := func(n int) any {
		var v any = map[string]any{}
		for i := 0; i < n-1; i++ {
			v = map[string]any{"a": v}
		}
		return v
	}
	deepSlice := func(n int) any {
		var v any = []any{}
		for i := 0; i < n-1; i++ {
			v = []any{v}
		}
		return v
	}
	for name, build := range map[string]func(int) any{"map": deepMap, "slice": deepSlice} {
		if _, err := node.FromAny(build(node.MaxDepth)); err != nil {
			t.Errorf("%s at MaxDepth: %v", name, err)
		}
		if _, err := node.FromAny(build(node.MaxDepth + 1)); !errors.Is(err, node.ErrTooDeep) {
			t.Errorf("%s at MaxDepth+1: err = %v, want ErrTooDeep", name, err)
		}
	}
}

// A self-referencing map needs no visited set: depth rises monotonically, so
// the cycle hits MaxDepth and comes back as a recoverable ErrTooDeep instead of
// killing the process.
func TestFromAnyCycleIsErrTooDeepNotAStackOverflow(t *testing.T) {
	selfMap := map[string]any{}
	selfMap["self"] = selfMap
	selfSlice := []any{nil}
	selfSlice[0] = selfSlice
	mutualA := map[string]any{}
	mutualB := map[string]any{"a": mutualA}
	mutualA["b"] = mutualB

	for name, in := range map[string]any{
		"self map":   selfMap,
		"self slice": selfSlice,
		"mutual":     mutualA,
	} {
		if _, err := node.FromAny(in); !errors.Is(err, node.ErrTooDeep) {
			t.Errorf("%s: err = %v, want ErrTooDeep", name, err)
		}
	}
}

// Whatever FromAny returns must satisfy the model invariant, so the value can go
// straight into Encode. Validate is the rule Encode itself applies.
func TestFromAnyOutputIsValid(t *testing.T) {
	in := map[string]any{
		"ints":   []any{1, int8(2), uint64(math.MaxUint64), new(big.Int).Lsh(big.NewInt(1), 100)},
		"floats": []any{1.5, float32(0.5), math.MaxFloat64},
		"text":   map[string]any{"utf8": "héllo 日本語 🎉", "empty": ""},
		"bools":  []any{true, false},
		"null":   nil,
		"nested": nodetest.OM("keep", []any{map[string]any{"deep": json.Number("7")}}),
		"empty":  map[string]any{},
		"earr":   []any{},
	}
	got, err := node.FromAny(in)
	if err != nil {
		t.Fatalf("FromAny: %v", err)
	}
	if err := node.Validate(got); err != nil {
		t.Fatalf("FromAny output failed Validate: %v", err)
	}

	// A top-level scalar converts fine — the container-root rule belongs to
	// Validate and Encode, not to the converter, which is also what lets a
	// caller convert a fragment.
	scalar, err := node.FromAny(42)
	if err != nil {
		t.Fatalf("FromAny(42): %v", err)
	}
	if scalar != int64(42) {
		t.Errorf("FromAny(42) = %#v", scalar)
	}
	if err := node.Validate(scalar); !errors.Is(err, node.ErrTopLevelScalar) {
		t.Errorf("Validate of a converted scalar = %v, want ErrTopLevelScalar", err)
	}

	// Invalid UTF-8 also stays Validate's business: the converter does not
	// inspect string contents, so the rejection still happens, one layer later.
	bad, err := node.FromAny(map[string]any{"k": string([]byte{0x84})})
	if err != nil {
		t.Fatalf("FromAny with invalid UTF-8: %v", err)
	}
	if err := node.Validate(bad); err == nil {
		t.Error("Validate accepted invalid UTF-8 that came through FromAny")
	}
}

func TestToAnyShapes(t *testing.T) {
	n := nodetest.OM(
		"b", int64(2),
		"a", []node.Node{int64(1), "x", nil, true, 1.5},
		"m", nodetest.OM("inner", uint64(math.MaxUint64)),
		"big", new(big.Int).Lsh(big.NewInt(1), 100),
		"empty", nodetest.OM(),
		"earr", []node.Node{},
	)
	got := node.ToAny(n)
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("ToAny(*OrderedMap) = %T, want map[string]any", got)
	}
	if len(m) != 6 {
		t.Fatalf("map has %d keys, want 6", len(m))
	}
	if _, ok := m["a"].([]any); !ok {
		t.Errorf("array became %T, want []any", m["a"])
	}
	if _, ok := m["m"].(map[string]any); !ok {
		t.Errorf("nested mapping became %T, want map[string]any", m["m"])
	}
	if _, ok := m["empty"].(map[string]any); !ok {
		t.Errorf("empty mapping became %T, want map[string]any", m["empty"])
	}
	if arr, ok := m["earr"].([]any); !ok || arr == nil || len(arr) != 0 {
		t.Errorf("empty array became %#v, want an empty non-nil []any", m["earr"])
	}
	// Scalars keep their carrier: ToAny converts containers, it does not
	// re-encode numbers.
	if m["b"] != int64(2) {
		t.Errorf("int64 became %T", m["b"])
	}
	if m["m"].(map[string]any)["inner"] != uint64(math.MaxUint64) {
		t.Errorf("uint64 became %T", m["m"].(map[string]any)["inner"])
	}

	// The *big.Int is copied, so a caller mutating the result cannot reach back
	// into the Node it came from.
	b := m["big"].(*big.Int)
	want := new(big.Int).Lsh(big.NewInt(1), 100)
	if b.Cmp(want) != 0 {
		t.Fatalf("big.Int = %v, want %v", b, want)
	}
	b.SetInt64(0)
	orig, _ := n.Get("big")
	if orig.(*big.Int).Cmp(want) != 0 {
		t.Error("ToAny aliased the source *big.Int")
	}

	// A top-level array, and scalars passing straight through.
	if _, ok := node.ToAny([]node.Node{int64(1)}).([]any); !ok {
		t.Error("top-level array did not become []any")
	}
	for _, scalar := range []node.Node{nil, true, "x", int64(1), uint64(1), 1.5} {
		if got := node.ToAny(scalar); got != scalar {
			t.Errorf("ToAny(%#v) = %#v", scalar, got)
		}
	}
	// Typed nils degrade to untyped nil rather than being handed out as a
	// map[string]any that panics on use.
	if got := node.ToAny((*node.OrderedMap)(nil)); got != nil {
		t.Errorf("ToAny(nil *OrderedMap) = %#v, want nil", got)
	}
	if got := node.ToAny((*big.Int)(nil)); got != nil {
		t.Errorf("ToAny(nil *big.Int) = %#v, want nil", got)
	}
}

// ToAny output must be what encoding/json accepts, since feeding a Node to a
// library that reflects over native containers is the whole point.
func TestToAnyFeedsEncodingJSON(t *testing.T) {
	n := nodetest.OM("a", int64(1), "b", []node.Node{"x", nil, true})
	b, err := json.Marshal(node.ToAny(n))
	if err != nil {
		t.Fatalf("json.Marshal(ToAny(n)): %v", err)
	}
	// Key order is gone, so compare parsed shapes rather than text.
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back) != 2 {
		t.Errorf("round-tripped JSON = %s", b)
	}
}

// The pair round-trips: values survive, order does not. Order loss is the
// documented cost of the native-map representation, and node.Equal's unordered
// mode is exactly the comparison that ignores it.
func TestFromAnyToAnyRoundTrip(t *testing.T) {
	for name, n := range map[string]node.Node{
		"rich":  nodetest.RichMap(),
		"array": nodetest.Array(),
		"deep":  nodetest.DeepMaps(50),
	} {
		back, err := node.FromAny(node.ToAny(n))
		if err != nil {
			t.Fatalf("%s: FromAny(ToAny(n)): %v", name, err)
		}
		if !node.Equal(n, back, false) {
			t.Errorf("%s: round trip changed values:\noriginal: %#v\nback:     %#v", name, n, back)
		}
	}

	// Order really is lost, not accidentally preserved: a map whose insertion
	// order differs from sorted order comes back sorted, so the ordered
	// comparison must fail while the unordered one holds.
	n := nodetest.OM("zebra", int64(1), "apple", int64(2))
	back, err := node.FromAny(node.ToAny(n))
	if err != nil {
		t.Fatalf("FromAny: %v", err)
	}
	if node.Equal(n, back, true) {
		t.Error("ordered comparison passed; the test no longer proves order is lost")
	}
	if !node.Equal(n, back, false) {
		t.Error("unordered comparison failed; values were not preserved")
	}
}

// ToAny under KeepOrder is a deep copy and nothing else, because there is no
// ordered Go map for it to build. The option exists for symmetry with FromAny, so
// the honest contract is equivalence with Clone — asserted with the ORDERED
// comparison, since an unordered one would pass even if the order were dropped.
func TestToAnyKeepOrderEqualsClone(t *testing.T) {
	for name, n := range map[string]node.Node{
		"rich":     nodetest.RichMap(),
		"array":    nodetest.Array(),
		"deep":     nodetest.DeepMaps(50),
		"unsorted": nodetest.OM("zebra", int64(1), "apple", int64(2)),
	} {
		got := node.ToAny(n, node.KeepOrder())
		if !node.Equal(got, node.Clone(n), true) {
			t.Errorf("%s: ToAny(n, KeepOrder()) != Clone(n)", name)
		}
		// Equivalence to Clone means order survives, which is what separates this
		// from the default mode.
		if !node.Equal(got, n, true) {
			t.Errorf("%s: ToAny(n, KeepOrder()) lost order relative to n", name)
		}
	}

	// Deep, not shallow: the mappings are new objects, and the one mutable scalar
	// carrier is copied too. *big.Int is the only Node type a caller could mutate
	// through a shared pointer, so it is the whole reason this is Clone's job
	// rather than a cast.
	big1 := big.NewInt(1)
	big1.Lsh(big1, 100)
	in := nodetest.OM("n", big1)
	got := node.ToAny(in, node.KeepOrder())
	if got == node.Node(in) {
		t.Fatal("ToAny(n, KeepOrder()) returned the input mapping itself")
	}
	copied, _ := got.(*node.OrderedMap).Get("n")
	if copied.(*big.Int) == big1 {
		t.Error("*big.Int was shared with the input rather than copied")
	}
	if copied.(*big.Int).Cmp(big1) != 0 {
		t.Errorf("copied *big.Int = %v, want %v", copied, big1)
	}

	// The default mode still converts, so the option is doing the work rather than
	// the tree happening to be native already.
	native := node.ToAny(in)
	if _, ok := native.(map[string]any); !ok {
		t.Errorf("default ToAny = %T, want map[string]any", native)
	}
}

// No package-level state backs the mode, so concurrent conversions in different
// modes cannot interfere: an Option is a value the caller folds into a local
// options struct per call. Run under -race, this is the assertion that says so.
func TestFromAnyToAnyConcurrentInBothModes(t *testing.T) {
	// One shared input, read by every goroutine. FromAny does not write to its
	// argument, so sharing it is part of what is being tested.
	in := map[string]any{
		"cfg":   nodetest.OM("zebra", int64(1), "apple", int64(2)),
		"alpha": int64(1),
	}
	tree := nodetest.RichMap()

	const goroutines = 8
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		keepOrder := i%2 == 0
		wg.Add(1)
		go func() {
			defer wg.Done()
			var opts []node.Option
			want := `{"alpha":1,"cfg":{"apple":2,"zebra":1}}`
			if keepOrder {
				opts = []node.Option{node.KeepOrder()}
				want = `{"alpha":1,"cfg":{"zebra":1,"apple":2}}`
			}
			for j := 0; j < 50; j++ {
				got, err := node.FromAny(in, opts...)
				if err != nil {
					t.Errorf("FromAny: %v", err)
					return
				}
				b, err := json.Marshal(got)
				if err != nil {
					t.Errorf("json.Marshal: %v", err)
					return
				}
				if string(b) != want {
					t.Errorf("keepOrder=%v: got %s, want %s", keepOrder, b, want)
					return
				}
				node.ToAny(tree, opts...)
			}
		}()
	}
	wg.Wait()
}
