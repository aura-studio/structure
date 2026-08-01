package structure

import (
	"errors"
	"strings"
	"testing"
)

// The branch-level coverage tests live with the code they exercise, in each
// codec package (and in node/ for the data model). What stays here is what only
// the facade can assert: that a guarantee holds for EVERY format reached through
// Parse and Encode, rather than for one codec in isolation.

func TestParseAndEncodeDepthGuards(t *testing.T) {
	// Moderate nesting is accepted by every format that can express it.
	for _, tc := range []struct {
		f     Format
		input string
	}{
		{JSON, deepJSON(100)},
		{YAML, deepYAML(100)},
		{Lua, deepLua(100)},
		{Python, deepPython(100)},
		{JS, deepJS(100)},
		{XML, deepXML(100)},
	} {
		if _, err := Parse(tc.input, tc.f); err != nil {
			t.Errorf("Parse(%s, 100 levels): %v", tc.f, err)
		}
	}
	// Encoding a tree past the guard fails before touching any format writer.
	for _, f := range allFormats {
		if _, err := Encode(deepMaps(maxDepth+1), f); !errors.Is(err, ErrTooDeep) {
			t.Errorf("Encode(%s, %d levels) err = %v, want ErrTooDeep", f, maxDepth+1, err)
		}
	}
}

// One level past the limit must be ErrTooDeep for every parser, not a stack
// overflow (which is fatal and cannot be recovered) and not a generic syntax
// error. XML is absent because its own parse test covers the same boundary
// directly; YAML is checked separately below.
func TestParserDepthGuardsAtTheLimit(t *testing.T) {
	for _, tc := range []struct {
		f     Format
		input string
	}{
		{JSON, strings.Repeat(`{"a":`, maxDepth+1) + "1" + strings.Repeat("}", maxDepth+1)},
		{Lua, deepLua(maxDepth + 1)},
		{Python, deepPython(maxDepth + 1)},
		{JS, deepJS(maxDepth + 1)},
		{TOML, deepTOML(maxDepth + 1)},
		{XML, deepXML(maxDepth + 1)},
	} {
		if _, err := Parse(tc.input, tc.f); !errors.Is(err, ErrTooDeep) {
			t.Errorf("%s at %d levels: err = %v, want ErrTooDeep", tc.f, maxDepth+1, err)
		}
	}
	// yaml.v3 enforces a limit of its own first, and for block style it reports a
	// plain syntax error rather than anything we can map onto the sentinel. It
	// must still be an error. (Flow style does map, and codec/yaml pins that.)
	if _, err := Parse(deepYAML(maxDepth+1), YAML); err == nil {
		t.Errorf("YAML accepted %d levels", maxDepth+1)
	}
}

func TestInvalidUTF8IsRejectedAtBothBoundaries(t *testing.T) {
	// A stray continuation byte. The byte-oriented parsers (Python, Lua) used to
	// carry it into a string, and the rune-ranging encoders then substituted
	// U+FFFD — a silent round-trip corruption found by FuzzRoundTrip.
	stray := string([]byte{0x84})

	for _, f := range allFormats {
		var pe *ParseError
		if _, err := Parse(`{"`+stray+`":[]}`, f); !errors.As(err, &pe) || pe.Format != f {
			t.Errorf("Parse(%s) of invalid UTF-8 = %v (%T), want a *ParseError", f, err, err)
		}
	}
	// The offset is reported as a position, not swallowed.
	var pe *ParseError
	if _, err := Parse("{\n\""+stray+"\":1}", JSON); errors.As(err, &pe) {
		if pe.Line != 2 || pe.Column != 2 {
			t.Errorf("invalid UTF-8 reported at line %d, col %d, want 2,2", pe.Line, pe.Column)
		}
	}
	// Valid UTF-8 that merely looks exotic still parses.
	if _, err := Parse(`{"héllo":"日本語 🎉"}`, JSON); err != nil {
		t.Errorf("multibyte UTF-8 rejected: %v", err)
	}

	// Encode rejects stray bytes in both values and keys, for every target.
	for _, f := range allFormats {
		if _, err := Encode(om("root", om("k", stray)), f); err == nil {
			t.Errorf("Encode(%s) accepted an invalid UTF-8 value", f)
		}
		if _, err := Encode(om("root", om(stray, "v")), f); err == nil {
			t.Errorf("Encode(%s) accepted an invalid UTF-8 key", f)
		}
	}
	if err := validate(arr(stray)); err == nil {
		t.Error("validate accepted an invalid UTF-8 array element")
	}
	if off := firstInvalidUTF8("ok"); off != -1 {
		t.Errorf("firstInvalidUTF8(valid) = %d, want -1", off)
	}
	// A truncated multi-byte prefix is invalid but decodes with size > 1 only
	// for genuine runes; the scan must still land on the offending offset.
	if got := firstInvalidUTF8("ab" + string([]byte{0xe4, 0xb8})); got != 2 {
		t.Errorf("firstInvalidUTF8(truncated) = %d, want 2", got)
	}
}
