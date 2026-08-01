// Package structure converts structured data between seven text formats —
// JSON, XML, YAML, TOML, Lua, Python and JavaScript — with a single call:
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
// code (AST-whitelisted parsing). XML maps attributes to "@name" keys and
// text to "#text", keeps values as strings, and requires one root element.
// TOML and XML cannot represent top-level arrays (ErrUnsupportedStructure).
// NaN/±Inf are encodable only to YAML and TOML. See the README for the full
// capability matrix and known limitations.
//
// All exported functions are safe for concurrent use; no package-level
// mutable state exists.
package structure

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Parse decodes input in format f into a Node tree. input must be valid UTF-8:
// every format here specifies UTF-8 source text, and the byte-oriented parsers
// (Python, Lua) would otherwise carry raw bytes that no encoder can reproduce.
func Parse(input string, f Format) (Node, error) {
	if off := firstInvalidUTF8(input); off >= 0 {
		line, col := xmlLineCol(input, int64(off))
		return nil, newParseError(f, line, col, "input is not valid UTF-8")
	}
	var (
		n   Node
		err error
	)
	switch f {
	case JSON:
		n, err = parseJSON(input)
	case XML:
		n, err = parseXML(input)
	case YAML:
		n, err = parseYAML(input)
	case TOML:
		n, err = parseTOML(input)
	case Lua:
		n, err = parseLua(input)
	case Python:
		n, err = parsePython(input)
	case JS:
		n, err = parseJS(input)
	default:
		return nil, fmt.Errorf("structure: unsupported format %v (supported: %s)", f, formatList())
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
	return wrapParseError(f, 0, 0, err, strings.TrimPrefix(err.Error(), "structure: "))
}

// Encode renders Node n as text in format f. The node is validated first
// (legal types, depth, root shape).
func Encode(n Node, f Format) (string, error) {
	switch f {
	case JSON:
		return encodeJSON(n)
	case XML:
		return encodeXML(n)
	case YAML:
		return encodeYAML(n)
	case TOML:
		return encodeTOML(n)
	case Lua:
		return encodeLua(n)
	case Python:
		return encodePython(n)
	case JS:
		return encodeJS(n)
	}
	return "", fmt.Errorf("structure: unsupported format %v (supported: %s)", f, formatList())
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
