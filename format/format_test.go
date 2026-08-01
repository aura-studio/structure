package format_test

import (
	"strings"
	"testing"

	"github.com/aura-studio/structure/v2/format"
)

func TestFormatString(t *testing.T) {
	want := map[format.Format]string{
		format.JSON:   "json",
		format.XML:    "xml",
		format.YAML:   "yaml",
		format.TOML:   "toml",
		format.Lua:    "lua",
		format.Python: "python",
		format.JS:     "js",
	}
	for f, s := range want {
		if got := f.String(); got != s {
			t.Errorf("Format(%d).String() = %q, want %q", int(f), got, s)
		}
	}
	if got := format.Format(99).String(); got != "Format(99)" {
		t.Errorf("out-of-range String() = %q", got)
	}
	if got := format.Format(-1).String(); got != "Format(-1)" {
		t.Errorf("negative String() = %q", got)
	}
}

func TestParseCanonical(t *testing.T) {
	for _, f := range format.All() {
		got, err := format.Parse(f.String())
		if err != nil {
			t.Errorf("Parse(%q) error: %v", f.String(), err)
			continue
		}
		if got != f {
			t.Errorf("Parse(%q) = %v, want %v", f.String(), got, f)
		}
	}
}

func TestParseAliases(t *testing.T) {
	cases := map[string]format.Format{
		"YML":        format.YAML,
		"yml":        format.YAML,
		"JavaScript": format.JS,
		"JAVASCRIPT": format.JS,
		"py":         format.Python,
		"PY":         format.Python,
		"python3":    format.Python,
		"ECMAScript": format.JS,
		"  json ":    format.JSON, // trimmed
		"Xml":        format.XML,
	}
	for in, want := range cases {
		got, err := format.Parse(in)
		if err != nil {
			t.Errorf("Parse(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("Parse(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseUnknown(t *testing.T) {
	_, err := format.Parse("csv")
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

func TestAllCoverage(t *testing.T) {
	all := format.All()
	if len(all) != 7 {
		t.Fatalf("All() has %d entries, want 7", len(all))
	}
	seen := map[format.Format]bool{}
	for _, f := range all {
		seen[f] = true
	}
	if len(seen) != 7 {
		t.Errorf("All() has duplicates: %v", all)
	}
}

// TestAllReturnsAFreshSlice pins the copy: All must not hand out the package's
// own backing array, or a caller could reorder every other caller's view.
func TestAllReturnsAFreshSlice(t *testing.T) {
	first := format.All()
	first[0] = format.JS
	second := format.All()
	if second[0] != format.JSON {
		t.Fatalf("All()[0] = %v after a caller mutated an earlier result; want json", second[0])
	}
}

// TestOrdinalsAreStable pins the numeric values of the Format constants. They
// are a wire format: the committed fuzz corpus under testdata/fuzz/ stores a raw
// selector byte that indexes this sequence, so renumbering would silently
// repoint every seed at a different parser.
func TestOrdinalsAreStable(t *testing.T) {
	want := map[format.Format]int{
		format.JSON: 0, format.XML: 1, format.YAML: 2, format.TOML: 3,
		format.Lua: 4, format.Python: 5, format.JS: 6,
	}
	for f, ordinal := range want {
		if int(f) != ordinal {
			t.Errorf("%v has ordinal %d, want %d", f, int(f), ordinal)
		}
	}
	if all := format.All(); len(all) != len(want) {
		t.Fatalf("All() has %d entries but %d ordinals are pinned", len(all), len(want))
	}
}

func TestList(t *testing.T) {
	got := format.List()
	if want := "json, xml, yaml, toml, lua, python, js"; got != want {
		t.Fatalf("List() = %q, want %q", got, want)
	}
}

func TestPreservesOrder(t *testing.T) {
	// TOML table order, XML attribute order and Lua hash order are unspecified
	// by their own specifications; the other four must preserve key order.
	want := map[format.Format]bool{
		format.JSON: true, format.YAML: true, format.Python: true, format.JS: true,
		format.XML: false, format.TOML: false, format.Lua: false,
	}
	for f, ordered := range want {
		if got := format.PreservesOrder(f); got != ordered {
			t.Errorf("PreservesOrder(%v) = %v, want %v", f, got, ordered)
		}
	}
	if got := format.PreservesOrder(format.Format(99)); got {
		t.Error("PreservesOrder(unknown) = true, want false")
	}
}
