package node

import (
	"errors"
	"fmt"
)

// MsgPrefix prefixes every error message this module produces. It names the
// MODULE, not this package: it must NOT become "node: " when code moves between
// packages here. The root facade strips it when folding a sentinel into a
// format.ParseError (whose own Error already prints "parse <format>: "), so the
// literal is a runtime contract, not cosmetics. Package format keeps its own
// copy of the same string — it deliberately does not import node — and the root
// facade pins both with exact-equality tests.
const MsgPrefix = "structure: "

// Sentinel errors describing what the Node model itself rejects. All of them
// are matched with errors.Is; ErrUnsupportedStructure additionally matches
// errors.ErrUnsupported.
//
// The duplicate-key sentinel is NOT here: OrderedMap.Set overwrites in place
// and has no notion of a duplicate, so that condition belongs to the parsers
// and lives in package format alongside ParseError.
var (
	// ErrUnsupportedStructure reports that a Node shape cannot be represented
	// in the target format (for example a top-level array encoded as TOML or
	// XML). It wraps errors.ErrUnsupported.
	ErrUnsupportedStructure = fmt.Errorf(MsgPrefix+"unsupported node shape for target format: %w", errors.ErrUnsupported)

	// ErrTooDeep reports nesting deeper than MaxDepth (10000). Parsers return
	// it instead of letting the goroutine stack overflow (a fatal error that
	// cannot be recovered).
	ErrTooDeep = errors.New(MsgPrefix + "nesting depth exceeds 10000")

	// ErrTopLevelScalar reports that the document (or Encode input) is a bare
	// scalar; the root must be a mapping (*OrderedMap) or an array ([]Node).
	ErrTopLevelScalar = errors.New(MsgPrefix + "top-level scalar is not supported (need a mapping or an array)")
)

// invalidf builds an "invalid node" error carrying the module prefix.
func invalidf(format string, args ...any) error {
	return fmt.Errorf(MsgPrefix+format, args...)
}
