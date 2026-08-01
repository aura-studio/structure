package json_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/aura-studio/structure/v2/codec/json"
	"github.com/aura-studio/structure/v2/format"
)

// A duplicate key used to surface as a bare sentinel with no position. It now
// carries the offending location while staying reachable via errors.Is.
func TestJSONDuplicateKeyCarriesPosition(t *testing.T) {
	var pe *format.ParseError
	err := func() error { _, err := json.Parse("{\"a\":1,\n\"a\":2}"); return err }()
	if !errors.Is(err, format.ErrDuplicateKey) {
		t.Fatalf("not ErrDuplicateKey: %v", err)
	}
	if !errors.As(err, &pe) {
		t.Fatalf("err is %T, want *format.ParseError wrapping the sentinel", err)
	}
	if pe.Line != 2 || pe.Column < 1 {
		t.Errorf("position = line %d col %d, want line 2 col >= 1", pe.Line, pe.Column)
	}
	if !strings.Contains(pe.Error(), "duplicate") {
		t.Errorf("error text %q lacks 'duplicate'", pe.Error())
	}
}

// A JSON syntax error used to arrive at 0,0 for some inputs.
func TestJSONParseErrorsCarryPosition(t *testing.T) {
	var pe *format.ParseError
	// Unterminated object: the syntax error points inside the document, not 0,0.
	_, err := json.Parse("{\"a\": ")
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v (%T)", err, err)
	}
	if pe.Format != format.JSON || pe.Line < 1 {
		t.Errorf("unterminated object: line=%d format=%s", pe.Line, pe.Format)
	}
	// A multi-line document reports the second line.
	if _, err := json.Parse("{\n  \"a\": ,\n}"); !errors.As(err, &pe) {
		t.Fatalf("err = %v (%T)", err, err)
	}
	if pe.Line != 2 {
		t.Errorf("bad token line = %d, want 2 (%v)", pe.Line, err)
	}
	// Trailing data after a complete document names the location of the junk.
	if _, err := json.Parse("{} garbage"); !errors.As(err, &pe) || pe.Column < 1 {
		t.Errorf("trailing data: %v (%T)", err, err)
	}
}
