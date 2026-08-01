package structure

import (
	"math"
	"math/big"
	"testing"
)

func TestRoundTrips(t *testing.T) {
	for _, f := range []Format{JSON, YAML, TOML, Lua, Python, JS} {
		f := f
		t.Run(f.String()+"_map", func(t *testing.T) { roundTrip(t, f, fixtureRichMap()) })
	}
	for _, f := range []Format{JSON, YAML, Lua, Python, JS} {
		f := f
		t.Run(f.String()+"_array", func(t *testing.T) { roundTrip(t, f, fixtureArray()) })
	}
	for _, f := range []Format{YAML, TOML} {
		f := f
		t.Run(f.String()+"_special", func(t *testing.T) { roundTrip(t, f, om("nan", math.NaN(), "pos", math.Inf(1), "neg", math.Inf(-1))) })
	}
	for _, f := range []Format{JSON, YAML, Python} {
		f := f
		t.Run(f.String()+"_big", func(t *testing.T) { roundTrip(t, f, om("big", new(big.Int).Lsh(big.NewInt(1), 100))) })
	}
}

func TestEquivalentChains(t *testing.T) {
	start := `{"name":"x","count":42,"ratio":3.5,"nested":{"v":7}}`
	viaYAML, e := Convert(start, JSON, YAML)
	if e != nil {
		t.Fatal(e)
	}
	viaTOML, e := Convert(viaYAML, YAML, TOML)
	if e != nil {
		t.Fatal(e)
	}
	a, e := Convert(viaTOML, TOML, JSON)
	if e != nil {
		t.Fatal(e)
	}
	direct, e := Convert(start, JSON, TOML)
	if e != nil {
		t.Fatal(e)
	}
	b, e := Convert(direct, TOML, JSON)
	if e != nil {
		t.Fatal(e)
	}
	an, _ := Parse(a, JSON)
	bn, _ := Parse(b, JSON)
	if !nodeEqual(an, bn, true) {
		t.Fatalf("chains differ\n%s\n%s", a, b)
	}
	// A string-only document survives the order-losing formats too: TOML sorts
	// inline-table keys and Lua sorts its hash segment, so this chain pins that
	// the VALUES arrive intact even where the order guarantee is waived.
	strStart := `{"a":"x","items":["1","2"],"t":{"k":"v"}}`
	viaLua, e := Convert(strStart, JSON, Lua)
	if e != nil {
		t.Fatal(e)
	}
	back, e := Convert(viaLua, Lua, JSON)
	if e != nil {
		t.Fatal(e)
	}
	orig, _ := Parse(strStart, JSON)
	got, _ := Parse(back, JSON)
	if !nodeEqual(orig, got, false) {
		t.Fatalf("Lua chain mismatch\n%s", back)
	}
}
