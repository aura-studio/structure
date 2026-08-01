package js_test

import (
	"errors"
	"math/big"
	"strconv"
	"testing"

	"github.com/aura-studio/structure/v2/codec/js"
	"github.com/aura-studio/structure/v2/format"
	"github.com/aura-studio/structure/v2/internal/nodetest"
	"github.com/aura-studio/structure/v2/node"
)

// Regressions for defects found by the adversarial audit.

// goja degrades integers past int64 to float64; the literal text is exact, so
// the parser recovers it.
func TestJSRecoversBigIntegersFromLiteralText(t *testing.T) {
	n, err := js.Parse(`({a: 18446744073709551616, b: 1267650600228229401496703205376, c: 1.5, d: 1e2, e: 1.0, f: 9007199254740993})`)
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*node.OrderedMap)
	for _, tc := range []struct {
		key  string
		want node.Node
	}{
		{"a", new(big.Int).Lsh(big.NewInt(1), 64)},
		{"b", new(big.Int).Lsh(big.NewInt(1), 100)},
		{"c", 1.5},
		{"d", 100.0}, // exponent form is a float in JS, and stays one
		{"e", 1.0},   // so is an explicit ".0"
		{"f", int64(9007199254740993)},
	} {
		got, _ := m.Get(tc.key)
		if !node.Equal(got, tc.want, true) {
			t.Errorf("%s = %T %#v, want %T %#v", tc.key, got, got, tc.want, tc.want)
		}
	}
	// Full round trip through the JS encoder.
	big2 := nodetest.OM("big", new(big.Int).Lsh(big.NewInt(1), 100))
	out, err := js.Encode(big2)
	if err != nil {
		t.Fatal(err)
	}
	back, err := js.Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if !node.Equal(big2, back, true) {
		t.Errorf("big int did not round-trip through JS: %#v (encoded %s)", back, out)
	}
}

// goja hands back a unistring.String that holds raw UTF-16 code units once the
// literal leaves ASCII; converting it with string() corrupted every non-ASCII
// value and key, and the encoder then rejected its own parse result.
func TestJSNonASCIIStringsSurvive(t *testing.T) {
	for _, s := range []string{
		"héllo", "日本語", "🎉", "a🎉b", "Привет", "ÿ", "￿",
		"mixed ascii + 中文 + 🎉", "\U0001F600\U0001F601",
	} {
		n, err := js.Parse(`({k: ` + strconv.Quote(s) + `})`)
		if err != nil {
			t.Errorf("parse %q: %v", s, err)
			continue
		}
		got, _ := n.(*node.OrderedMap).Get("k")
		if got != s {
			t.Errorf("value %q parsed as %q", s, got)
		}
		// The same corruption hit key position.
		kn, err := js.Parse(`({` + strconv.Quote(s) + `: 1})`)
		if err != nil {
			t.Errorf("parse key %q: %v", s, err)
			continue
		}
		if v, ok := kn.(*node.OrderedMap).Get(s); !ok || v != int64(1) {
			t.Errorf("key %q -> %#v", s, kn)
		}
		// Full round trip: the encoder used to reject the invalid UTF-8.
		out, err := js.Encode(nodetest.OM("k", s))
		if err != nil {
			t.Errorf("encode %q: %v", s, err)
			continue
		}
		back, err := js.Parse(out)
		if err != nil {
			t.Errorf("encoded %q does not re-parse: %v", s, err)
			continue
		}
		if !node.Equal(back, nodetest.OM("k", s), true) {
			t.Errorf("%q round-tripped to %#v", s, back)
		}
	}
	// A lone surrogate has no UTF-8 form; utf16.Decode would substitute U+FFFD.
	for name, input := range map[string]string{
		"lone high":     `({k: "\uD800"})`,
		"lone low":      `({k: "\uDC00"})`,
		"reversed pair": `({k: "\uDC00\uD800"})`,
		"high then eof": `({k: "a\uD83D"})`,
		"in key":        `({"\uD800": 1})`,
	} {
		if _, err := js.Parse(input); err == nil {
			t.Errorf("%s: accepted %s", name, input)
		}
	}
	// A correctly paired surrogate is just an astral character and is fine.
	n, err := js.Parse(`({k: "😀"})`)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := n.(*node.OrderedMap).Get("k"); v != "\U0001F600" {
		t.Errorf("surrogate pair -> %q", v)
	}
}

// A JS syntax error used to arrive with no position at all.
func TestJSSyntaxErrorCarriesPosition(t *testing.T) {
	var pe *format.ParseError
	if _, err := js.Parse(`({a: })`); !errors.As(err, &pe) {
		t.Fatalf("err = %v (%T), want *format.ParseError", err, err)
	}
	if pe.Format != format.JS || pe.Line != 1 || pe.Column < 1 {
		t.Errorf("position = %s line %d col %d", pe.Format, pe.Line, pe.Column)
	}
	// The prepended "(" must not be charged to a later line's column.
	if _, err := js.Parse("({\n  a: )\n})"); !errors.As(err, &pe) {
		t.Fatalf("err = %v (%T)", err, err)
	}
	if pe.Line != 2 || pe.Column < 1 {
		t.Errorf("multi-line position = line %d col %d", pe.Line, pe.Column)
	}
}
