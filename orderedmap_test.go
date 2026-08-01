package structure

import (
	"encoding/json"
	"math"
	"math/big"
	"testing"
)

func TestOrderedMapInsertionOrder(t *testing.T) {
	m := NewOrderedMap()
	m.Set("z", int64(1))
	m.Set("a", int64(2))
	m.Set("m", int64(3))
	want := []string{"z", "a", "m"}
	got := m.Keys()
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Keys() = %v, want %v", got, want)
		}
	}
	if m.Len() != 3 {
		t.Errorf("Len() = %d, want 3", m.Len())
	}
}

func TestOrderedMapSetUpdatesInPlace(t *testing.T) {
	m := om("a", int64(1), "b", int64(2), "c", int64(3))
	m.Set("b", int64(99))
	v, ok := m.Get("b")
	if !ok || v.(int64) != 99 {
		t.Fatalf("Get(b) = %v, %v", v, ok)
	}
	want := []string{"a", "b", "c"} // order preserved
	for i, k := range m.Keys() {
		if k != want[i] {
			t.Fatalf("order changed after Set: %v", m.Keys())
		}
	}
}

func TestOrderedMapDelete(t *testing.T) {
	m := om("a", int64(1), "b", int64(2), "c", int64(3), "d", int64(4))
	if !m.Delete("b") {
		t.Fatal("Delete(b) = false")
	}
	if m.Delete("b") {
		t.Fatal("second Delete(b) = true")
	}
	if !m.Delete("a") {
		t.Fatal("Delete(a) = false")
	}
	if !m.Delete("d") {
		t.Fatal("Delete(d) = false")
	}
	if got := m.Keys(); len(got) != 1 || got[0] != "c" {
		t.Fatalf("Keys() = %v, want [c]", got)
	}
	// Re-insert goes to the end.
	m.Set("z", int64(0))
	m.Set("c", int64(5)) // existing: stays first
	if got := m.Keys(); got[0] != "c" || got[1] != "z" {
		t.Fatalf("Keys() = %v, want [c z]", got)
	}
	if v, _ := m.Get("c"); v.(int64) != 5 {
		t.Fatal("c value lost")
	}
}

func TestOrderedMapAll(t *testing.T) {
	m := om("x", int64(1), "y", int64(2), "z", int64(3))
	var keys []string
	sum := int64(0)
	for k, v := range m.All() {
		keys = append(keys, k)
		sum += v.(int64)
	}
	if len(keys) != 3 || keys[0] != "x" || keys[2] != "z" || sum != 6 {
		t.Fatalf("All() keys=%v sum=%d", keys, sum)
	}
	// Early break.
	count := 0
	for range m.All() {
		count++
		if count == 1 {
			break
		}
	}
	if count != 1 {
		t.Fatalf("early break failed, count=%d", count)
	}
}

func TestOrderedMapCloneDeep(t *testing.T) {
	inner := om("deep", new(big.Int).SetInt64(7))
	m := om("inner", inner, "list", arr(int64(1), int64(2)))
	c := m.Clone()

	// Mutate the clone's nested structures.
	cInner, _ := c.Get("inner")
	cInner.(*OrderedMap).Set("deep", int64(8))
	cList, _ := c.Get("list")
	cList.([]Node)[0] = int64(99)

	// Original must be untouched.
	origInner, _ := m.Get("inner")
	if v, _ := origInner.(*OrderedMap).Get("deep"); v.(*big.Int).Int64() != 7 {
		t.Fatal("clone shares inner map with original")
	}
	origList, _ := m.Get("list")
	if origList.([]Node)[0].(int64) != 1 {
		t.Fatal("clone shares slice with original")
	}
}

func TestOrderedMapMarshalJSONOrder(t *testing.T) {
	m := om("z", int64(1), "a", "x", "m", arr(int64(1), nil, true))
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"z":1,"a":"x","m":[1,null,true]}`
	if string(b) != want {
		t.Fatalf("MarshalJSON = %s, want %s", b, want)
	}
}

func TestOrderedMapMarshalJSONNested(t *testing.T) {
	m := om("outer", om("b", int64(2), "a", int64(1)))
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"outer":{"b":2,"a":1}}` {
		t.Fatalf("got %s", b)
	}
}

func TestNodeEqualScalars(t *testing.T) {
	cases := []struct {
		a, b Node
		want bool
	}{
		{nil, nil, true},
		{nil, false, false},
		{true, true, true},
		{true, false, false},
		{"s", "s", true},
		{"s", "t", false},
		{int64(5), int64(5), true},
		{int64(5), uint64(5), true}, // cross-carrier numeric equality
		{uint64(5), big.NewInt(5), true},
		{int64(5), 5.0, false}, // int vs float: distinct
		{5.0, 5.0, true},
		{math.NaN(), math.NaN(), true}, // NaN == NaN by contract
		{math.Inf(1), math.Inf(1), true},
		{math.Inf(1), math.Inf(-1), false},
		{math.Float64frombits(1 << 63), 0.0, false}, // -0.0 vs +0.0
	}
	for i, c := range cases {
		if got := nodeEqual(c.a, c.b, true); got != c.want {
			t.Errorf("case %d: nodeEqual(%v, %v) = %v, want %v", i, c.a, c.b, got, c.want)
		}
	}
}

func TestNodeEqualContainers(t *testing.T) {
	a := om("k1", int64(1), "k2", arr(int64(1), "x"))
	b := om("k1", int64(1), "k2", arr(int64(1), "x"))
	if !nodeEqual(a, b, true) {
		t.Error("equal maps reported unequal")
	}
	// Order-sensitive.
	c := om("k2", arr(int64(1), "x"), "k1", int64(1))
	if nodeEqual(a, c, true) {
		t.Error("different order compared equal under ordered=true")
	}
	if !nodeEqual(a, c, false) {
		t.Error("same content compared unequal under ordered=false")
	}
	// Arrays are order-sensitive always.
	if nodeEqual(arr(int64(1), int64(2)), arr(int64(2), int64(1)), false) {
		t.Error("arrays with different order compared equal")
	}
	// Type mismatch container vs scalar.
	if nodeEqual(a, "x", false) {
		t.Error("map vs string compared equal")
	}
	if nodeEqual(arr(), om(), false) {
		t.Error("empty array vs empty map compared equal")
	}
}
