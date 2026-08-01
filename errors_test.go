package structure

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestParseErrorFormat(t *testing.T) {
	tests := []struct {
		name string
		err  *ParseError
		want string
	}{
		{
			name: "with line and col",
			err:  &ParseError{Format: JSON, Line: 3, Column: 12, Msg: "unexpected token"},
			want: "parse json: line 3, col 12: unexpected token",
		},
		{
			name: "line only",
			err:  &ParseError{Format: YAML, Line: 7, Msg: "bad indent"},
			want: "parse yaml: line 7, col 0: bad indent",
		},
		{
			name: "col only",
			err:  &ParseError{Format: Lua, Column: 4, Msg: "stray byte"},
			want: "parse lua: line 0, col 4: stray byte",
		},
		{
			name: "no position",
			err:  &ParseError{Format: Python, Msg: "trailing garbage"},
			want: "parse python: trailing garbage",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseErrorUnwrap(t *testing.T) {
	base := errors.New("boom")
	pe := wrapParseError(TOML, 1, 2, base, "decode failed")
	if !errors.Is(pe, base) {
		t.Error("errors.Is(pe, base) = false, want true")
	}
	if pe.Unwrap() != base {
		t.Error("Unwrap did not return the wrapped error")
	}
	if pe.Error() != "parse toml: line 1, col 2: decode failed" {
		t.Errorf("unexpected Error(): %q", pe.Error())
	}
}

func TestNewParseError(t *testing.T) {
	pe := newParseError(JS, 2, 9, "got %q, want %q", "x", "y")
	if pe.Msg != `got "x", want "y"` {
		t.Errorf("Msg = %q", pe.Msg)
	}
	if pe.Format != JS || pe.Line != 2 || pe.Column != 9 {
		t.Errorf("fields not set: %+v", pe)
	}
}

func TestSentinelErrorsIs(t *testing.T) {
	if !errors.Is(ErrUnsupportedStructure, errors.ErrUnsupported) {
		t.Error("ErrUnsupportedStructure must match errors.ErrUnsupported")
	}
	for _, e := range []error{ErrTooDeep, ErrTopLevelScalar, ErrDuplicateKey} {
		if !errors.Is(e, e) {
			t.Errorf("errors.Is(%v, itself) = false", e)
		}
	}
	// Wrapping must preserve matching.
	wrapped := fmt.Errorf("encode: %w", ErrTooDeep)
	if !errors.Is(wrapped, ErrTooDeep) {
		t.Error("wrapped ErrTooDeep not matched")
	}
	wrapped2 := fmt.Errorf("context: %w", ErrUnsupportedStructure)
	if !errors.Is(wrapped2, errors.ErrUnsupported) {
		t.Error("wrapped ErrUnsupportedStructure must still match errors.ErrUnsupported")
	}
}

func TestSentinelErrorMessages(t *testing.T) {
	wantSub := map[error]string{
		ErrTooDeep:        "10000",
		ErrTopLevelScalar: "top-level scalar",
		ErrDuplicateKey:   "duplicate",
	}
	for e, sub := range wantSub {
		if got := e.Error(); !strings.Contains(got, sub) {
			t.Errorf("%q does not contain %q", got, sub)
		}
	}
}
