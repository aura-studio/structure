package format

import (
	"errors"
	"fmt"
)

// ErrDuplicateKey reports a repeated mapping key in formats where duplicates
// are rejected (JSON, YAML, Python).
//
// It lives here rather than in package node because the Node model has no
// notion of a duplicate: OrderedMap.Set overwrites an existing key in place.
// Detecting a repeat is a parser concern, so the sentinel belongs beside
// ParseError.
var ErrDuplicateKey = errors.New(MsgPrefix + "duplicate mapping key")

// ParseError describes a syntax or semantic failure while parsing a document.
// Format names the source format; Line and Column are 1-based when the parser
// can determine them and 0 otherwise. Column counts bytes, not runes, so it
// addresses the exact input offset even when the surrounding text is not valid
// UTF-8. Msg is an English, developer-facing description.
type ParseError struct {
	Format Format
	Line   int
	Column int
	Msg    string

	err error // wrapped underlying error, if any
}

// Error renders the canonical form "parse <format>: line L, col C: msg",
// omitting the line/col segment when both are unknown.
func (e *ParseError) Error() string {
	loc := ""
	if e.Line > 0 || e.Column > 0 {
		loc = fmt.Sprintf("line %d, col %d: ", e.Line, e.Column)
	}
	return fmt.Sprintf("parse %s: %s%s", e.Format, loc, e.Msg)
}

// Unwrap exposes the wrapped underlying error to errors.Is/errors.As.
func (e *ParseError) Unwrap() error { return e.err }

// New builds a *ParseError for format f with positional info and no wrapped
// error.
func New(f Format, line, col int, format string, args ...any) *ParseError {
	return &ParseError{Format: f, Line: line, Column: col, Msg: fmt.Sprintf(format, args...)}
}

// Wrap builds a *ParseError that wraps err, so errors.Is and errors.As reach
// through it. Codecs use this for failures originating in a third-party parser
// and for sentinel-carrying errors; the wrapped error is unreachable otherwise,
// since the field is unexported and a composite literal in another package
// cannot set it.
func Wrap(f Format, line, col int, err error, msg string) *ParseError {
	return &ParseError{Format: f, Line: line, Column: col, Msg: msg, err: err}
}

// DuplicateKey builds a positioned *ParseError for a repeated mapping key. The
// sentinel stays reachable through errors.Is because Unwrap exposes it.
func DuplicateKey(f Format, line, col int, key string) *ParseError {
	return &ParseError{
		Format: f,
		Line:   line,
		Column: col,
		Msg:    fmt.Sprintf("duplicate mapping key %q", key),
		err:    ErrDuplicateKey,
	}
}

// LineCol converts a byte offset into a 1-based line and column. It lives here
// because its only purpose is to fill ParseError.Line and ParseError.Column:
// every caller feeds the result straight into New, Wrap or DuplicateKey.
//
// The column counts bytes rather than runes, matching ParseError.Column, so the
// position stays exact even for input that is not valid UTF-8.
func LineCol(input string, offset int64) (int, int) {
	if offset < 0 {
		return 0, 0
	}
	if int(offset) > len(input) {
		offset = int64(len(input))
	}
	line, col := 1, 1
	for i := int64(0); i < offset; i++ {
		if input[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}
