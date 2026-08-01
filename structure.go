// Package structure converts structured data between six text formats —
// JSON, YAML, TOML, Lua, Python and JavaScript — with a single call:
//
//	out, err := structure.Convert(input, structure.JSON, structure.YAML)
//
// All conversions are text in, text out (UTF-8 strings). Internally every
// document is parsed into a Node tree and re-encoded:
//
//	Convert(input, from, to) == Encode(Parse(input, from), to)
//
// # Node model
//
// A Node is one of: *OrderedMap (mapping), []Node (array), nil, bool, int64,
// uint64, *big.Int, float64, or string. Documents must be rooted at a
// mapping or an array; bare top-level scalars are rejected. Integers ride an
// int64 -> uint64 -> *big.Int ladder so values beyond 2^53 survive JSON
// round-trips exactly. Time/date values normalize to strings.
//
// # Format notes
//
// Lua, Python and JavaScript accept literal subsets only and never execute
// code (AST-whitelisted parsing). TOML cannot represent a top-level array or a
// nil value (ErrUnsupportedStructure). NaN/±Inf are encodable only to YAML and
// TOML. See the README for the full capability matrix and known limitations.
//
// # Package layout
//
// This package is a facade. The data model lives in [node], the format enum and
// parse errors in [format], and one parser/encoder pair per format under
// codec/. Every name below is a type alias or a thin forwarder, so
// structure.OrderedMap and node.OrderedMap are the same type and either
// spelling satisfies errors.As, json.Marshaler and a type switch alike. Import
// the subpackages directly when you want one format only; import this package
// for the format-dispatching API.
//
// All exported functions are safe for concurrent use; no package-level
// mutable state exists.
package structure

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/aura-studio/structure/v2/codec/js"
	"github.com/aura-studio/structure/v2/codec/json"
	"github.com/aura-studio/structure/v2/codec/lua"
	"github.com/aura-studio/structure/v2/codec/python"
	"github.com/aura-studio/structure/v2/codec/toml"
	"github.com/aura-studio/structure/v2/codec/yaml"
	"github.com/aura-studio/structure/v2/format"
	"github.com/aura-studio/structure/v2/node"
)

// Node is the intermediate representation shared by Parse and Encode. A valid
// Node is exactly one of:
//
//	*OrderedMap  mapping, insertion-ordered
//	[]Node       array
//	nil          null
//	bool         boolean
//	int64        integer
//	uint64       integer beyond int64
//	*big.Int     integer beyond uint64
//	float64      floating point
//	string       text
//
// Any other Go type is rejected by Encode. Documents must be rooted at a
// mapping or an array.
//
// It is an alias for [node.Node], so the two spellings are interchangeable.
type Node = node.Node

// OrderedMap is an insertion-ordered string-keyed map with O(1) Get, Set and
// Delete. It is an alias for [node.OrderedMap]: the methods are declared there,
// and a value produced by either package satisfies both spellings.
type OrderedMap = node.OrderedMap

// NewOrderedMap returns an empty OrderedMap ready for use.
func NewOrderedMap() *OrderedMap { return node.NewOrderedMap() }

// Format identifies one of the six supported data formats. It is an alias for
// [format.Format].
type Format = format.Format

// The six supported formats. These are aliases of the [format] package's
// constants, so the ordinals match exactly; the committed fuzz corpus depends on
// them, so they must never be renumbered.
const (
	JSON   = format.JSON
	YAML   = format.YAML
	TOML   = format.TOML
	Lua    = format.Lua
	Python = format.Python
	JS     = format.JS
)

// ParseError describes a syntax or semantic failure while parsing a document.
// It is an alias for [format.ParseError], so errors.As works with either
// spelling.
type ParseError = format.ParseError

// Sentinel errors returned across the package. All of them are matchable with
// errors.Is, including through a *ParseError, which unwraps to the sentinel it
// was built from.
var (
	// ErrUnsupportedStructure reports a Node shape the target format cannot
	// represent (a top-level array for TOML, nil for TOML, an empty array for
	// Lua). It wraps errors.ErrUnsupported.
	ErrUnsupportedStructure = node.ErrUnsupportedStructure

	// ErrTooDeep reports nesting beyond the fixed 10000-level limit.
	ErrTooDeep = node.ErrTooDeep

	// ErrTopLevelScalar reports a document whose root is a scalar rather than a
	// mapping or an array.
	ErrTopLevelScalar = node.ErrTopLevelScalar

	// ErrDuplicateKey reports a repeated mapping key in a format that rejects
	// duplicates (JSON, YAML, Python).
	ErrDuplicateKey = format.ErrDuplicateKey
)

// ParseFormat resolves a format name case-insensitively, accepting common
// aliases (yml, javascript, py, python3, ecmascript). Unknown names yield an
// error listing the supported formats.
func ParseFormat(s string) (Format, error) { return format.Parse(s) }

// Parse decodes input in format f into a Node tree. input must be valid UTF-8:
// every format here specifies UTF-8 source text, and the byte-oriented parsers
// (Python, Lua) would otherwise carry raw bytes that no encoder can reproduce.
func Parse(input string, f Format) (Node, error) {
	if off := firstInvalidUTF8(input); off >= 0 {
		line, col := format.LineCol(input, int64(off))
		return nil, format.New(f, line, col, "input is not valid UTF-8")
	}
	var (
		n   Node
		err error
	)
	switch f {
	case JSON:
		n, err = json.Parse(input)
	case YAML:
		n, err = yaml.Parse(input)
	case TOML:
		n, err = toml.Parse(input)
	case Lua:
		n, err = lua.Parse(input)
	case Python:
		n, err = python.Parse(input)
	case JS:
		n, err = js.Parse(input)
	default:
		return nil, fmt.Errorf("%sunsupported format %v (supported: %s)", node.MsgPrefix, f, format.List())
	}
	if err != nil {
		return nil, parseErrorFor(f, err)
	}
	return n, nil
}

// parseErrorFor labels a parser failure with the format it came from. Parsers
// return bare sentinels for the whole-document rejections (ErrTooDeep,
// ErrTopLevelScalar) and plain errors for a few internal ones, none of which
// tell the caller which format failed. Wrapping adds Format while leaving
// errors.Is/errors.As intact through ParseError.Unwrap. The redundant
// "structure: " prefix is dropped because ParseError.Error already prints
// "parse <format>: ".
func parseErrorFor(f Format, err error) error {
	var pe *ParseError
	if errors.As(err, &pe) {
		return err
	}
	return format.Wrap(f, 0, 0, err, strings.TrimPrefix(err.Error(), node.MsgPrefix))
}

// Encode renders Node n as text in format f. The node is validated first
// (legal types, depth, root shape).
func Encode(n Node, f Format) (string, error) {
	switch f {
	case JSON:
		return json.Encode(n)
	case YAML:
		return yaml.Encode(n)
	case TOML:
		return toml.Encode(n)
	case Lua:
		return lua.Encode(n)
	case Python:
		return python.Encode(n)
	case JS:
		return js.Encode(n)
	}
	return "", fmt.Errorf("%sunsupported format %v (supported: %s)", node.MsgPrefix, f, format.List())
}

// firstInvalidUTF8 returns the byte offset of the first invalid UTF-8 sequence
// in s, or -1 when s is entirely valid.
func firstInvalidUTF8(s string) int {
	if utf8.ValidString(s) {
		return -1
	}
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size <= 1 {
			return i
		}
		i += size
	}
	return -1
}

// Convert parses input as format `from` and re-encodes it as format `to`.
func Convert(input string, from, to Format) (string, error) {
	n, err := Parse(input, from)
	if err != nil {
		return "", err
	}
	return Encode(n, to)
}
