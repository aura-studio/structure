package tests

import (
	"encoding/json"
	"math/big"
	"reflect"
	"strings"
	"testing"
)

// The type-by-type behaviour of FromAny/ToAny is pinned in package node, which
// owns them. What only the facade can assert is the part node cannot see: that a
// converted tree is actually accepted by every one of the six encoders, and that
// ToAny takes what Parse hands back. A converter that produced a subtly illegal
// carrier would still satisfy node's own tests and fail here.

func TestFromAnyOutputEncodesToEveryFormat(t *testing.T) {
	// Deliberately inside the intersection of all six formats: a mapping root
	// (TOML rejects a top-level array), no nil (TOML has no null and Lua rejects
	// nil array elements), no empty array (Lua), integers within int64 (TOML's
	// ceiling). Those exclusions are format limits, not converter limits.
	in := map[string]any{
		"s":   "héllo 日本語 🎉",
		"i":   42,
		"b":   true,
		"f":   1.5,
		"arr": []any{int8(1), int16(2), uint32(3)},
		"m":   map[string]any{"nested": 7, "deep": map[string]any{"x": "y"}},
	}
	n, err := FromAny(in)
	if err != nil {
		t.Fatalf("FromAny: %v", err)
	}
	if err := validate(n); err != nil {
		t.Fatalf("FromAny output failed validate: %v", err)
	}
	for _, f := range allFormats {
		text, err := Encode(n, f)
		if err != nil {
			t.Fatalf("Encode(%s) of FromAny output: %v", f, err)
		}
		back, err := Parse(text, f)
		if err != nil {
			t.Fatalf("Parse(%s) of that text: %v\n%s", f, err, text)
		}
		if !nodeEqual(n, back, ordered(f)) {
			t.Errorf("%s: round trip changed the tree\nencoded:\n%s", f, text)
		}
	}
}

func TestToAnyAcceptsParseOutputFromEveryFormat(t *testing.T) {
	n := om("s", "x", "i", int64(1), "arr", arr(int64(1), int64(2)), "m", om("k", true))
	for _, f := range allFormats {
		text, err := Encode(n, f)
		if err != nil {
			t.Fatalf("Encode(%s): %v", f, err)
		}
		parsed, err := Parse(text, f)
		if err != nil {
			t.Fatalf("Parse(%s): %v", f, err)
		}
		native := ToAny(parsed)
		m, ok := native.(map[string]any)
		if !ok {
			t.Fatalf("ToAny(Parse(%s)) = %T, want map[string]any", f, native)
		}
		if _, ok := m["arr"].([]any); !ok {
			t.Errorf("%s: array became %T, want []any", f, m["arr"])
		}
		if _, ok := m["m"].(map[string]any); !ok {
			t.Errorf("%s: nested mapping became %T, want map[string]any", f, m["m"])
		}
		// encoding/json is the main reason to want native containers, and it
		// rejects anything it cannot reflect over — so marshaling doubles as a
		// check that no library type leaked through.
		if _, err := json.Marshal(native); err != nil {
			t.Errorf("%s: json.Marshal(ToAny(...)): %v", f, err)
		}
		// Back through FromAny, values survive; order does not (Go maps have
		// none to give back).
		again, err := FromAny(native)
		if err != nil {
			t.Fatalf("%s: FromAny(ToAny(...)): %v", f, err)
		}
		if !nodeEqual(parsed, again, false) {
			t.Errorf("%s: any round trip changed values", f)
		}
	}
}

// The full loop a caller writes: native map in, text out, text in, native map
// out. The expectation is spelled in carrier types because that is the contract
// — int normalizes to int64 and stays there.
func TestNativeMapThroughTextAndBack(t *testing.T) {
	in := map[string]any{"a": 1, "b": []any{"x", 2.5}, "c": map[string]any{"d": false}}
	n, err := FromAny(in)
	if err != nil {
		t.Fatalf("FromAny: %v", err)
	}
	text, err := Encode(n, JSON)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// Keys come out sorted — the deterministic order FromAny substitutes for the
	// Go map's absent one. Compared against an explicitly-ordered map rather than
	// a text literal, so the assertion is about key order and not about the
	// encoder's indentation.
	sorted, err := Encode(om("a", int64(1), "b", arr("x", 2.5), "c", om("d", false)), JSON)
	if err != nil {
		t.Fatalf("Encode(sorted): %v", err)
	}
	if text != sorted {
		t.Errorf("Encode = %s\nwant (sorted keys) = %s", text, sorted)
	}
	parsed, err := Parse(text, JSON)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := map[string]any{"a": int64(1), "b": []any{"x", 2.5}, "c": map[string]any{"d": false}}
	if got := ToAny(parsed); !reflect.DeepEqual(got, want) {
		t.Errorf("ToAny = %#v, want %#v", got, want)
	}
}

// json.Number exists because encoding/json otherwise routes every number
// through float64, which is the same problem this module's carrier ladder
// solves. Accepting it means a caller can hand off a decoded document without
// losing integers past 2^53 — verified through Encode, where the loss would show.
func TestFromAnyPreservesBigIntegersFromEncodingJSON(t *testing.T) {
	const text = `{"big":123456789012345678901234567890,"int53":9007199254740993}`
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var native any
	if err := dec.Decode(&native); err != nil {
		t.Fatalf("decode: %v", err)
	}
	n, err := FromAny(native)
	if err != nil {
		t.Fatalf("FromAny: %v", err)
	}
	m := n.(*OrderedMap)
	big1, _ := m.Get("big")
	if b, ok := big1.(*big.Int); !ok {
		t.Errorf("big carrier = %T, want *big.Int", big1)
	} else if want, _ := new(big.Int).SetString("123456789012345678901234567890", 10); b.Cmp(want) != 0 {
		t.Errorf("big = %v, want %v", b, want)
	}
	if v, _ := m.Get("int53"); v != int64(9007199254740993) {
		t.Errorf("int53 = %#v, want int64(9007199254740993)", v)
	}
	// Encoded back out, both survive as integer literals rather than degrading to
	// 1.2345678901234568e+29 and 9007199254740992 the way a float64 detour would.
	out, err := Encode(n, JSON)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	for _, digits := range []string{"123456789012345678901234567890", "9007199254740993"} {
		if !strings.Contains(out, digits) {
			t.Errorf("Encode output lost %s:\n%s", digits, out)
		}
	}
	if strings.ContainsAny(out, "eE") {
		t.Errorf("Encode used exponent notation, so an integer went through float64:\n%s", out)
	}
}

// Whether KeepOrder reaches the TEXT is a question only the facade can answer:
// package node owns the option but cannot see the encoders. This also pins where
// the promise stops — TOML and Lua give table order no meaning, and
// format.PreservesOrder is the single source of truth for which formats carry it
// through, so the expectation is derived from it rather than from a second list.
func TestKeepOrderReachesEncodedText(t *testing.T) {
	// Insertion order differs from sorted order, so the two modes are
	// distinguishable. Without that, a forwarder that dropped the option would
	// still pass.
	in := map[string]any{"cfg": om("zebra", int64(1), "apple", int64(2))}

	def, err := FromAny(in)
	if err != nil {
		t.Fatalf("FromAny: %v", err)
	}
	keep, err := FromAny(in, KeepOrder())
	if err != nil {
		t.Fatalf("FromAny(KeepOrder): %v", err)
	}

	for _, f := range allFormats {
		defText, err := Encode(def, f)
		if err != nil {
			t.Fatalf("Encode(%s) default: %v", f, err)
		}
		keepText, err := Encode(keep, f)
		if err != nil {
			t.Fatalf("Encode(%s) KeepOrder: %v", f, err)
		}

		// In an order-preserving format the two modes must produce different
		// text, which is the whole feature. In TOML and Lua they need not, and
		// nothing here demands they do.
		if ordered(f) && defText == keepText {
			t.Errorf("%s: KeepOrder produced the same text as the default:\n%s", f, keepText)
		}

		// Either way values survive the trip, compared with the key-order rule
		// that format actually claims.
		back, err := Parse(keepText, f)
		if err != nil {
			t.Fatalf("Parse(%s) of KeepOrder output: %v\n%s", f, err, keepText)
		}
		if !nodeEqual(keep, back, ordered(f)) {
			t.Errorf("%s: KeepOrder round trip changed values:\n%s", f, keepText)
		}
	}
}

// The option reaches ToAny through the facade too. It cannot produce an ordered
// Go map — nothing can — so what it does here is leave the mappings alone, which
// is observable as the carrier type of the result.
func TestFacadeToAnyForwardsKeepOrder(t *testing.T) {
	n, err := FromAny(map[string]any{"cfg": om("zebra", int64(1), "apple", int64(2))}, KeepOrder())
	if err != nil {
		t.Fatalf("FromAny: %v", err)
	}

	if _, ok := ToAny(n).(map[string]any); !ok {
		t.Errorf("ToAny default = %T, want map[string]any", ToAny(n))
	}
	kept := ToAny(n, KeepOrder())
	if _, ok := kept.(*OrderedMap); !ok {
		t.Fatalf("ToAny(KeepOrder) = %T, want *OrderedMap", kept)
	}
	// A deep copy and nothing more: order intact, and not the input itself.
	if !nodeEqual(kept, n, true) {
		t.Error("ToAny(n, KeepOrder()) is not equal to n with order compared")
	}
	if kept == n {
		t.Error("ToAny(n, KeepOrder()) returned the input tree rather than a copy")
	}
}

// Conversion is an explicit step, not something Parse or Encode does for you.
// These two assertions are the boundary: adding native-container support to the
// codecs would silently make FromAny optional and both of these would fail.
func TestEncodeStillRejectsNativeContainers(t *testing.T) {
	for _, f := range allFormats {
		if _, err := Encode(map[string]any{"a": 1}, f); err == nil {
			t.Errorf("Encode(%s) accepted a bare map[string]any", f)
		}
		if _, err := Encode([]any{1, 2}, f); err == nil {
			t.Errorf("Encode(%s) accepted a []any of native values", f)
		}
	}
	// Parse never produces one either, so a caller who skips ToAny cannot end up
	// with a native map by accident.
	n, err := Parse(`{"a":{"b":[1]}}`, JSON)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, ok := n.(*OrderedMap); !ok {
		t.Fatalf("Parse returned %T, want *OrderedMap", n)
	}
}
