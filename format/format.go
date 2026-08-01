// Package format identifies the seven text formats this module converts
// between, and describes a parse failure in one of them.
//
// It is the second root of the module's import graph: like package node it
// depends on nothing else here, and deliberately does not import node — a
// format name and a parse position say nothing about the data model. Codecs
// import both.
package format

import (
	"fmt"
	"strings"
)

// MsgPrefix prefixes every error message this module produces. It names the
// MODULE, not this package: it must NOT become "format: ". Package node holds
// an identical copy (the two packages are independent roots and neither imports
// the other), and the root facade pins both with exact-equality tests, because
// the root's Parse strips this prefix when folding a bare sentinel into a
// ParseError and a silent rename would corrupt the public error text.
const MsgPrefix = "structure: "

// Format identifies one of the seven supported data formats.
type Format int

// The seven supported formats.
//
// The ordinals are a wire format, not an implementation detail: the committed
// fuzz corpus under testdata/fuzz/ stores a raw selector byte that indexes this
// sequence, so a reordering would silently repoint every seed at a different
// parser. Append only; never renumber.
const (
	JSON Format = iota
	XML
	YAML
	TOML
	Lua
	Python
	JS
)

var names = [...]string{
	JSON:   "json",
	XML:    "xml",
	YAML:   "yaml",
	TOML:   "toml",
	Lua:    "lua",
	Python: "python",
	JS:     "js",
}

// all lists every Format in declaration order. It is unexported and copied by
// All so the package keeps no exported mutable state.
var all = [...]Format{JSON, XML, YAML, TOML, Lua, Python, JS}

// aliases maps lowercase aliases (and canonical names) to Formats.
var aliases = map[string]Format{
	"json":       JSON,
	"xml":        XML,
	"yaml":       YAML,
	"yml":        YAML,
	"toml":       TOML,
	"lua":        Lua,
	"python":     Python,
	"py":         Python,
	"python3":    Python,
	"js":         JS,
	"javascript": JS,
	"ecmascript": JS,
}

// String returns the canonical lowercase name of the format.
func (f Format) String() string {
	if int(f) < 0 || int(f) >= len(names) {
		return fmt.Sprintf("Format(%d)", int(f))
	}
	return names[f]
}

// All returns every supported Format in declaration order. The slice is freshly
// allocated, so callers cannot mutate the package's state.
func All() []Format {
	out := make([]Format, len(all))
	copy(out, all[:])
	return out
}

// List renders every supported format name, for error messages.
func List() string {
	out := make([]string, len(all))
	for i, f := range all {
		out[i] = f.String()
	}
	return strings.Join(out, ", ")
}

// Parse resolves a format name case-insensitively, accepting common aliases
// (yml, javascript, py, python3, ecmascript). Unknown names yield an error
// listing the supported formats.
func Parse(s string) (Format, error) {
	if f, ok := aliases[strings.ToLower(strings.TrimSpace(s))]; ok {
		return f, nil
	}
	return 0, fmt.Errorf(MsgPrefix+"unknown format %q (supported: %s)", s, List())
}

// PreservesOrder reports whether f gives mapping key order a defined meaning.
// TOML table order, XML attribute order and Lua hash order are all
// unspecified by their own specifications, so a round trip through those three
// may reorder keys; the other four must preserve them exactly.
func PreservesOrder(f Format) bool {
	switch f {
	case JSON, YAML, Python, JS:
		return true
	}
	return false
}
