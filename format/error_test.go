package format_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/aura-studio/structure/v2/format"
)

func TestParseErrorFormat(t *testing.T) {
	tests := []struct {
		name string
		err  *format.ParseError
		want string
	}{
		{
			name: "with line and col",
			err:  &format.ParseError{Format: format.JSON, Line: 3, Column: 12, Msg: "unexpected token"},
			want: "parse json: line 3, col 12: unexpected token",
		},
		{
			name: "line only",
			err:  &format.ParseError{Format: format.YAML, Line: 7, Msg: "bad indent"},
			want: "parse yaml: line 7, col 0: bad indent",
		},
		{
			name: "col only",
			err:  &format.ParseError{Format: format.Lua, Column: 4, Msg: "stray byte"},
			want: "parse lua: line 0, col 4: stray byte",
		},
		{
			name: "no position",
			err:  &format.ParseError{Format: format.Python, Msg: "trailing garbage"},
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
	pe := format.Wrap(format.TOML, 1, 2, base, "decode failed")
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

// TestParseErrorUnwrapNil pins that a ParseError built without a wrapped error
// unwraps to nil rather than to itself, so errors.Is terminates.
func TestParseErrorUnwrapNil(t *testing.T) {
	pe := format.New(format.JSON, 1, 1, "boom")
	if pe.Unwrap() != nil {
		t.Errorf("Unwrap() = %v, want nil", pe.Unwrap())
	}
	if errors.Is(pe, errors.New("boom")) {
		t.Error("a bare ParseError must not match an unrelated error")
	}
}

func TestNew(t *testing.T) {
	pe := format.New(format.JS, 2, 9, "got %q, want %q", "x", "y")
	if pe.Msg != `got "x", want "y"` {
		t.Errorf("Msg = %q", pe.Msg)
	}
	if pe.Format != format.JS || pe.Line != 2 || pe.Column != 9 {
		t.Errorf("fields not set: %+v", pe)
	}
}

func TestDuplicateKey(t *testing.T) {
	pe := format.DuplicateKey(format.YAML, 4, 2, "dup")
	if !errors.Is(pe, format.ErrDuplicateKey) {
		t.Error("DuplicateKey must match ErrDuplicateKey through errors.Is")
	}
	want := `parse yaml: line 4, col 2: duplicate mapping key "dup"`
	if got := pe.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestErrDuplicateKeyMessage(t *testing.T) {
	// The prefix names the module, not this package: it must never become
	// "format: ". The root facade's contract test pins the exact string; here we
	// only guard the two halves.
	got := format.ErrDuplicateKey.Error()
	if !strings.HasPrefix(got, format.MsgPrefix) {
		t.Errorf("%q does not start with %q", got, format.MsgPrefix)
	}
	if !strings.Contains(got, "duplicate") {
		t.Errorf("%q does not mention duplicate", got)
	}
}

func TestLineCol(t *testing.T) {
	const input = "ab\ncd\n\nxyz"
	cases := []struct {
		offset    int64
		line, col int
	}{
		{0, 1, 1},
		{1, 1, 2},
		{2, 1, 3},  // the newline itself
		{3, 2, 1},  // first byte of line 2
		{5, 2, 3},  // the newline ending line 2
		{6, 3, 1},  // the empty line
		{7, 4, 1},  // first byte of line 4
		{10, 4, 4}, // one past the end
	}
	for _, c := range cases {
		line, col := format.LineCol(input, c.offset)
		if line != c.line || col != c.col {
			t.Errorf("LineCol(offset=%d) = (%d, %d), want (%d, %d)", c.offset, line, col, c.line, c.col)
		}
	}
	// A negative offset is unknown, not position 1:1.
	if line, col := format.LineCol(input, -1); line != 0 || col != 0 {
		t.Errorf("LineCol(-1) = (%d, %d), want (0, 0)", line, col)
	}
	// An offset past the end clamps to the end rather than panicking.
	if line, col := format.LineCol(input, 9999); line != 4 || col != 4 {
		t.Errorf("LineCol(9999) = (%d, %d), want (4, 4)", line, col)
	}
}

// TestLineColCountsBytes pins the documented byte-not-rune contract: the column
// must address the exact input offset even when the text is multi-byte, so a
// position stays usable on input that is not valid UTF-8.
func TestLineColCountsBytes(t *testing.T) {
	input := "é=1" // 'é' is two bytes
	line, col := format.LineCol(input, 2)
	if line != 1 || col != 3 {
		t.Fatalf("LineCol = (%d, %d), want (1, 3) counting bytes", line, col)
	}
}
