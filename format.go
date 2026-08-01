package structure

import (
	"fmt"
	"strings"
)

// Format identifies one of the seven supported data formats.
type Format int

// The seven supported formats.
const (
	JSON Format = iota
	XML
	YAML
	TOML
	Lua
	Python
	JS
)

var formatNames = [...]string{
	JSON:   "json",
	XML:    "xml",
	YAML:   "yaml",
	TOML:   "toml",
	Lua:    "lua",
	Python: "python",
	JS:     "js",
}

// allFormats lists every Format in declaration order; parsers and encoders
// dispatch over it.
var allFormats = []Format{JSON, XML, YAML, TOML, Lua, Python, JS}

// formatAliases maps lowercase aliases (and canonical names) to Formats.
var formatAliases = map[string]Format{
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
	if int(f) < 0 || int(f) >= len(formatNames) {
		return fmt.Sprintf("Format(%d)", int(f))
	}
	return formatNames[f]
}

// ParseFormat resolves a format name case-insensitively, accepting common
// aliases (yml, javascript, py, python3, ecmascript). Unknown names yield an
// error listing the supported formats.
func ParseFormat(s string) (Format, error) {
	if f, ok := formatAliases[strings.ToLower(strings.TrimSpace(s))]; ok {
		return f, nil
	}
	return 0, fmt.Errorf("structure: unknown format %q (supported: %s)", s, formatList())
}

// formatList renders every supported format name, for error messages.
func formatList() string {
	names := make([]string, len(allFormats))
	for i, f := range allFormats {
		names[i] = f.String()
	}
	return strings.Join(names, ", ")
}
