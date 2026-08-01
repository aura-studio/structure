package structure

import (
	"errors"
	"fmt"
)

// Sentinel errors returned across the package. All of them are matched with
// errors.Is; ErrUnsupportedStructure additionally matches errors.ErrUnsupported.
var (
	// ErrUnsupportedStructure reports that a Node shape cannot be represented
	// in the target format (for example a top-level array encoded as TOML or
	// XML). It wraps errors.ErrUnsupported.
	ErrUnsupportedStructure = fmt.Errorf("structure: unsupported node shape for target format: %w", errors.ErrUnsupported)

	// ErrTooDeep reports nesting deeper than maxDepth (10000). Parsers return
	// it instead of letting the goroutine stack overflow (a fatal error that
	// cannot be recovered).
	ErrTooDeep = errors.New("structure: nesting depth exceeds 10000")

	// ErrTopLevelScalar reports that the document (or Encode input) is a bare
	// scalar; the root must be a mapping (*OrderedMap) or an array ([]Node).
	ErrTopLevelScalar = errors.New("structure: top-level scalar is not supported (need a mapping or an array)")

	// ErrDuplicateKey reports a repeated mapping key in formats where
	// duplicates are rejected (JSON, YAML, Python).
	ErrDuplicateKey = errors.New("structure: duplicate mapping key")
)

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

// newParseError builds a *ParseError for format f with positional info.
func newParseError(f Format, line, col int, format string, args ...any) *ParseError {
	return &ParseError{Format: f, Line: line, Column: col, Msg: fmt.Sprintf(format, args...)}
}

// duplicateKeyError builds a positioned *ParseError for a repeated mapping key.
// The sentinel stays reachable through errors.Is because ParseError.Unwrap
// exposes it; callers used to get a bare sentinel with no position at all.
func duplicateKeyError(f Format, line, col int, key string) *ParseError {
	return &ParseError{
		Format: f,
		Line:   line,
		Column: col,
		Msg:    fmt.Sprintf("duplicate mapping key %q", key),
		err:    ErrDuplicateKey,
	}
}

// wrapParseError builds a *ParseError that wraps err.
func wrapParseError(f Format, line, col int, err error, msg string) *ParseError {
	return &ParseError{Format: f, Line: line, Column: col, Msg: msg, err: err}
}
