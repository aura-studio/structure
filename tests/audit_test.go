package tests

import (
	"errors"
	"strings"
	"testing"
)

// Regressions for the defects found by the adversarial audit that are the
// FACADE's to hold: cross-format agreement, format attribution, and the
// whole-document rejections Parse and Encode own. The per-codec regressions
// live in each codec package's own audit_test.go.

// The YAML limit used to sit one level below everyone else's, because a scalar
// leaf was charged as a nesting level: the very same document was accepted as
// JSON and rejected as YAML. The comparison must use one text that is valid in
// both formats, or the shapes differ and the asymmetry hides (a pure sequence
// has no scalar leaf and never showed it). This belongs at the facade because
// the invariant is agreement BETWEEN two codecs, not a property of either one.
func TestYAMLDepthMatchesJSON(t *testing.T) {
	for _, shape := range []struct {
		name       string
		open, leaf string
		close      string
	}{
		{"mapping with scalar leaf", `{"a":`, "1", "}"},
		{"sequence with scalar leaf", "[", "1", "]"},
		{"sequence, empty leaf", "[", "", "]"},
	} {
		for _, d := range []int{maxDepth - 1, maxDepth, maxDepth + 1} {
			input := strings.Repeat(shape.open, d) + shape.leaf + strings.Repeat(shape.close, d)
			_, jsonErr := Parse(input, JSON)
			_, yamlErr := Parse(input, YAML)
			jsonDeep := errors.Is(jsonErr, ErrTooDeep)
			yamlDeep := errors.Is(yamlErr, ErrTooDeep)
			if jsonDeep != yamlDeep {
				t.Errorf("%s at depth %d: JSON too-deep=%v but YAML too-deep=%v (json=%v, yaml=%v)",
					shape.name, d, jsonDeep, yamlDeep, jsonErr, yamlErr)
			}
			if want := d > maxDepth; jsonDeep != want {
				t.Errorf("%s at depth %d: too-deep=%v, want %v", shape.name, d, jsonDeep, want)
			}
		}
	}
}

// Only the JS parser should say "js"; the shared negation helper used to
// attribute every unary-minus error to Lua. Both parsers drive the same
// carrier-arithmetic helper in package node, so the attribution is only
// observable by comparing the two — hence a facade test.
func TestUnaryMinusErrorNamesItsOwnFormat(t *testing.T) {
	for _, tc := range []struct {
		f     Format
		input string
	}{
		{JS, `({a: -"x"})`},
		{Lua, `{a = -"x"}`},
	} {
		var pe *ParseError
		_, err := Parse(tc.input, tc.f)
		if !errors.As(err, &pe) {
			t.Errorf("%s unary minus error = %v (%T), want *ParseError", tc.f, err, err)
			continue
		}
		if pe.Format != tc.f {
			t.Errorf("%s unary minus error names format %v", tc.f, pe.Format)
		}
	}
}

func TestUnsupportedFormatListsNames(t *testing.T) {
	_, perr := Parse("x", Format(999))
	if perr == nil || !strings.Contains(perr.Error(), "json") || !strings.Contains(perr.Error(), "lua") {
		t.Errorf("Parse unsupported format = %v, want a list including json and lua", perr)
	}
	if _, eerr := Encode(om(), Format(999)); eerr == nil || !strings.Contains(eerr.Error(), "toml") {
		t.Errorf("Encode unsupported format = %v, want a list including toml", eerr)
	}
}

// Parse used to return bare sentinels for the whole-document rejections, so the
// caller could not tell which format had failed. They are now *ParseError with
// Format set, and errors.Is still reaches the sentinel.
func TestParseSentinelsCarryFormat(t *testing.T) {
	deep := strings.Repeat("[", maxDepth+1) + strings.Repeat("]", maxDepth+1)
	for _, tc := range []struct {
		name  string
		f     Format
		input string
		want  error
	}{
		{"json scalar", JSON, "42", ErrTopLevelScalar},
		{"yaml scalar", YAML, "42", ErrTopLevelScalar},
		{"lua scalar", Lua, "42", ErrTopLevelScalar},
		{"python scalar", Python, "42", ErrTopLevelScalar},
		{"js scalar", JS, "42", ErrTopLevelScalar},
		{"json deep", JSON, deep, ErrTooDeep},
		{"yaml deep", YAML, deep, ErrTooDeep},
		{"python deep", Python, deep, ErrTooDeep},
	} {
		_, err := Parse(tc.input, tc.f)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: errors.Is(%v, %v) = false", tc.name, err, tc.want)
			continue
		}
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Errorf("%s: want a *ParseError, got %T (%v)", tc.name, err, err)
			continue
		}
		if pe.Format != tc.f {
			t.Errorf("%s: Format = %v, want %v", tc.name, pe.Format, tc.f)
		}
		if strings.Contains(pe.Msg, "structure: ") {
			t.Errorf("%s: message keeps the redundant package prefix: %q", tc.name, pe.Msg)
		}
	}
}

// The duplicate-key sentinel must be reachable through the facade, with the
// position intact, for the three parsers that raise it themselves. The per-codec
// position details are pinned in each codec's own tests; what the facade adds is
// that Parse does not flatten the sentinel on its way out.
//
// TOML is deliberately absent: BurntSushi detects the duplicate itself and
// reports "Key 'a' has already been defined", which the codec passes through as
// an ordinary positioned *ParseError. It is an error either way, but it does not
// match ErrDuplicateKey. Mapping it would mean string-matching a dependency's
// message, so it is left as a documented asymmetry rather than a fragile fix.
func TestDuplicateKeyReachesTheFacade(t *testing.T) {
	for _, tc := range []struct {
		f     Format
		input string
	}{
		{JSON, "{\"a\":1,\n\"a\":2}"},
		{YAML, "a: 1\na: 2\n"},
		{Python, "{\"a\": 1,\n\"a\": 2}"},
	} {
		_, err := Parse(tc.input, tc.f)
		if !errors.Is(err, ErrDuplicateKey) {
			t.Errorf("%s: errors.Is(%v, ErrDuplicateKey) = false", tc.f, err)
			continue
		}
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Errorf("%s: err is %T, want *ParseError wrapping the sentinel", tc.f, err)
			continue
		}
		if pe.Format != tc.f {
			t.Errorf("%s: Format = %v", tc.f, pe.Format)
		}
		if pe.Line != 2 || pe.Column < 1 {
			t.Errorf("%s: position = line %d col %d, want line 2 col >= 1", tc.f, pe.Line, pe.Column)
		}
		if !strings.Contains(pe.Error(), "duplicate") {
			t.Errorf("%s: error text %q lacks 'duplicate'", tc.f, pe.Error())
		}
	}
	// TOML still rejects the document, just without the sentinel.
	if _, err := Parse("a = 1\na = 2\n", TOML); err == nil {
		t.Error("TOML accepted a duplicate key")
	}
}
