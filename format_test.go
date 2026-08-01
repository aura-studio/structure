package structure

import (
	"strings"
	"testing"
)

func TestFormatString(t *testing.T) {
	want := map[Format]string{
		JSON:   "json",
		XML:    "xml",
		YAML:   "yaml",
		TOML:   "toml",
		Lua:    "lua",
		Python: "python",
		JS:     "js",
	}
	for f, s := range want {
		if got := f.String(); got != s {
			t.Errorf("Format(%d).String() = %q, want %q", int(f), got, s)
		}
	}
	if got := Format(99).String(); got != "Format(99)" {
		t.Errorf("out-of-range String() = %q", got)
	}
	if got := Format(-1).String(); got != "Format(-1)" {
		t.Errorf("negative String() = %q", got)
	}
}

func TestParseFormatCanonical(t *testing.T) {
	for _, f := range allFormats {
		got, err := ParseFormat(f.String())
		if err != nil {
			t.Errorf("ParseFormat(%q) error: %v", f.String(), err)
			continue
		}
		if got != f {
			t.Errorf("ParseFormat(%q) = %v, want %v", f.String(), got, f)
		}
	}
}

func TestParseFormatAliases(t *testing.T) {
	cases := map[string]Format{
		"YML":        YAML,
		"yml":        YAML,
		"JavaScript": JS,
		"JAVASCRIPT": JS,
		"py":         Python,
		"PY":         Python,
		"python3":    Python,
		"ECMAScript": JS,
		"  json ":    JSON, // trimmed
		"Xml":        XML,
	}
	for in, want := range cases {
		got, err := ParseFormat(in)
		if err != nil {
			t.Errorf("ParseFormat(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseFormat(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseFormatUnknown(t *testing.T) {
	_, err := ParseFormat("csv")
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
	msg := err.Error()
	for _, name := range []string{"json", "xml", "yaml", "toml", "lua", "python", "js", "csv"} {
		if !strings.Contains(msg, name) {
			t.Errorf("error message %q should mention %q", msg, name)
		}
	}
}

func TestAllFormatsCoverage(t *testing.T) {
	if len(allFormats) != 7 {
		t.Fatalf("allFormats has %d entries, want 7", len(allFormats))
	}
	seen := map[Format]bool{}
	for _, f := range allFormats {
		seen[f] = true
	}
	if len(seen) != 7 {
		t.Errorf("allFormats has duplicates: %v", allFormats)
	}
}
