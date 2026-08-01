package structure

import (
	"errors"
	"math/big"
	"strconv"
	"strings"
	"testing"
)

// Regressions for the defects found by the adversarial audit. Each test names
// the behaviour that used to be wrong, so a future change that reintroduces it
// fails here rather than silently corrupting data.

// YAML's own depth cap fires before ours; the error must still be the sentinel
// the package documents (Requirement 8.1).
func TestYAMLDepthReturnsErrTooDeep(t *testing.T) {
	for name, input := range map[string]string{
		"flow sequence": strings.Repeat("[", maxDepth+1) + strings.Repeat("]", maxDepth+1),
		"flow mapping":  strings.Repeat("{a: ", maxDepth+1) + "1" + strings.Repeat("}", maxDepth+1),
	} {
		if _, err := parseYAML(input); !errors.Is(err, ErrTooDeep) {
			t.Errorf("%s: err = %v, want ErrTooDeep", name, err)
		}
	}
	// Within the limit still parses.
	if _, err := parseYAML(strings.Repeat("[", 1000) + strings.Repeat("]", 1000)); err != nil {
		t.Errorf("1000 levels rejected: %v", err)
	}
}

// The YAML limit used to sit one level below everyone else's, because a scalar
// leaf was charged as a nesting level: the very same document was accepted as
// JSON and rejected as YAML. The comparison must use one text that is valid in
// both formats, or the shapes differ and the asymmetry hides (a pure sequence
// has no scalar leaf and never showed it).
func TestYAMLDepthMatchesJSON(t *testing.T) {
	for _, shape := range []struct {
		name       string
		open, leaf string
		close      string
	}{
		{"mapping with scalar leaf", `{"a":`, "1", "}"},
		{"sequence with scalar leaf", "[", "1", "]"},
		{"sequence, empty leaf", "[", "", "]"},
	} {
		for _, d := range []int{maxDepth - 1, maxDepth, maxDepth + 1} {
			input := strings.Repeat(shape.open, d) + shape.leaf + strings.Repeat(shape.close, d)
			_, jsonErr := Parse(input, JSON)
			_, yamlErr := Parse(input, YAML)
			jsonDeep := errors.Is(jsonErr, ErrTooDeep)
			yamlDeep := errors.Is(yamlErr, ErrTooDeep)
			if jsonDeep != yamlDeep {
				t.Errorf("%s at depth %d: JSON too-deep=%v but YAML too-deep=%v (json=%v, yaml=%v)",
					shape.name, d, jsonDeep, yamlDeep, jsonErr, yamlErr)
			}
			if want := d > maxDepth; jsonDeep != want {
				t.Errorf("%s at depth %d: too-deep=%v, want %v", shape.name, d, jsonDeep, want)
			}
		}
	}
}

// yaml.v3 implements YAML 1.1 only and rejects a %YAML 1.2 directive. The
// library deliberately surfaces that instead of stripping the directive and
// parsing under 1.1 rules, which would silently change how y/no and 0o777-style
// scalars resolve. Documented as a known limitation, so pin the behaviour.
func TestYAMLVersionDirective(t *testing.T) {
	if _, err := Parse("%YAML 1.1\n---\na: 1\n", YAML); err != nil {
		t.Errorf("1.1 directive rejected: %v", err)
	}
	for _, in := range []string{"%YAML 1.2\n---\na: 1\n", "%YAML 1.3\n---\na: 1\n"} {
		var pe *ParseError
		_, err := Parse(in, YAML)
		if !errors.As(err, &pe) {
			t.Errorf("%q: want a *ParseError, got %T (%v)", in, err, err)
			continue
		}
		if pe.Format != YAML {
			t.Errorf("%q: Format = %v, want YAML", in, pe.Format)
		}
	}
}

// yaml.v3 picks a scalar style that silently loses data for three kinds of
// string; the encoder must force double quotes for them.
func TestYAMLStringStylesRoundTrip(t *testing.T) {
	for _, s := range []string{
		"\nx", "\n", "\n\n", "x\n", "a\n\tb", "a\tb", "\ttab", "a\rb",
		"<<", "true", "null", "", " lead", "trail ", "a: b", "- x", "#c", "~",
	} {
		out, err := Encode(om("k", s), YAML)
		if err != nil {
			t.Errorf("Encode(%q): %v", s, err)
			continue
		}
		back, err := Parse(out, YAML)
		if err != nil {
			t.Errorf("Parse of encoded %q failed: %v\nencoded: %q", s, err, out)
			continue
		}
		got, _ := back.(*OrderedMap).Get("k")
		if gs, ok := got.(string); !ok || gs != s {
			t.Errorf("%q round-tripped to %#v (encoded %q)", s, got, out)
		}
	}
	// "<<" also has to survive in key position.
	out, err := Encode(om("<<", "v"), YAML)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(out, YAML)
	if err != nil {
		t.Fatalf("encoded %q does not re-parse: %v", out, err)
	}
	if v, ok := back.(*OrderedMap).Get("<<"); !ok || v != "v" {
		t.Errorf(`"<<" key round-tripped to %#v (encoded %q)`, v, out)
	}
	if yamlPlainSafe("<<") || yamlPlainSafe("\nx") || yamlPlainSafe("a\tb") {
		t.Error("yamlPlainSafe accepted a style that loses data")
	}
	if !yamlPlainSafe("plain") {
		t.Error("yamlPlainSafe needlessly quotes an ordinary string")
	}
}

// Only a plain << is a merge key; a quoted "<<" is an ordinary string key.
func TestYAMLMergeKeyOnlyWhenPlain(t *testing.T) {
	for _, input := range []string{"<<: {a: 1}\nb: 2\n", "<<: *ref\n"} {
		if _, err := parseYAML(input); err == nil {
			t.Errorf("merge key accepted: %q", input)
		}
	}
	for _, input := range []string{`"<<": v` + "\n", `'<<': v` + "\n"} {
		n, err := parseYAML(input)
		if err != nil {
			t.Errorf("quoted merge-looking key rejected: %q: %v", input, err)
			continue
		}
		if v, ok := n.(*OrderedMap).Get("<<"); !ok || v != "v" {
			t.Errorf("%q -> %#v", input, n)
		}
	}
	// A plain << in *value* position is yaml.v3's !!merge tag too, and is not a
	// key, so it must be rejected rather than silently mistyped.
	if _, err := parseYAML("k: <<\n"); err == nil {
		t.Error("plain << value accepted")
	}
	if n, err := parseYAML("k: \"<<\"\n"); err != nil {
		t.Errorf("quoted << value rejected: %v", err)
	} else if v, _ := n.(*OrderedMap).Get("k"); v != "<<" {
		t.Errorf(`quoted "<<" value -> %#v`, v)
	}
}

// An explicit tag is authoritative: the big-integer recovery that compensates
// for yaml.v3's !!float degradation must not retype a hand-written !!float.
func TestYAMLExplicitTagsAreHonoured(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  Node
	}{
		{"a: !!float 1\n", 1.0},
		{"a: !!float 1.5\n", 1.5},
		{"a: !!float -3\n", -3.0},
		{"a: !!int 7\n", int64(7)},
		{"a: !!str 1\n", "1"},
		{"a: !!null\n", nil},
		{"a: !!null ~\n", nil},
		// Implicit degradation is still recovered as an exact integer.
		{"a: 18446744073709551616\n", new(big.Int).Lsh(big.NewInt(1), 64)},
	} {
		n, err := parseYAML(tc.input)
		if err != nil {
			t.Errorf("%q: %v", tc.input, err)
			continue
		}
		got, _ := n.(*OrderedMap).Get("a")
		if !nodeEqual(got, tc.want, true) {
			t.Errorf("%q -> %T %#v, want %T %#v", tc.input, got, got, tc.want, tc.want)
		}
	}
	// A tag that contradicts its content is an error, not a silent coercion.
	for _, input := range []string{"a: !!int 1.5\n", "a: !!int x\n", "a: !!null x\n"} {
		if _, err := parseYAML(input); err == nil {
			t.Errorf("contradictory tag accepted: %q", input)
		}
	}
}

// encoding/xml writes malformed tags without complaining; the encoder has to
// reject names and characters XML 1.0 cannot represent.
func TestXMLEncoderRejectsMalformedOutput(t *testing.T) {
	for name, key := range map[string]string{
		"space in name":  "a b",
		"angle bracket":  "a>b",
		"leading digit":  "1a",
		"empty name":     "",
		"bare at":        "@",
		"quote in name":  `a"b`,
		"slash in name":  "a/b",
		"equals in name": "a=b",
		// A colon is a legal Name character, but the parser reads element names
		// as Name.Local, so <a:b> would come back as the key "b".
		"prefixed name": "a:b",
		"bare colon":    ":",
	} {
		if _, err := Encode(om("root", om(key, "x")), XML); !errors.Is(err, ErrUnsupportedStructure) {
			t.Errorf("%s (%q): err = %v, want ErrUnsupportedStructure", name, key, err)
		}
	}
	// Characters XML 1.0 forbids, in text and in attribute position.
	for _, bad := range []string{"x\x01y", "\x00", "x\x1fy", "￾"} {
		if _, err := Encode(om("root", om("a", bad)), XML); !errors.Is(err, ErrUnsupportedStructure) {
			t.Errorf("text %q: err = %v, want ErrUnsupportedStructure", bad, err)
		}
		if _, err := Encode(om("root", om("@a", bad)), XML); !errors.Is(err, ErrUnsupportedStructure) {
			t.Errorf("attribute %q: err = %v, want ErrUnsupportedStructure", bad, err)
		}
	}
	// Legal names and the whitespace XML does allow still encode, and re-parse.
	for _, tc := range []struct{ key, val string }{
		{"a", "x"}, {"_a", "x"}, {"a-b", "x"}, {"a.b", "x"}, {"a1", "x"},
		{"名前", "x"}, {"a", "tab\there"}, {"a", "nl\nhere"},
	} {
		src := om("root", om(tc.key, tc.val))
		out, err := Encode(src, XML)
		if err != nil {
			t.Errorf("Encode(%q=%q): %v", tc.key, tc.val, err)
			continue
		}
		if _, err := Parse(out, XML); err != nil {
			t.Errorf("encoded %q=%q does not re-parse: %v\n%s", tc.key, tc.val, err, out)
		}
	}
	// Namespace declarations keep their colon form and stay encodable.
	out, err := Encode(om("root", om("@xmlns", "urn:x", "@xmlns:p", "urn:y")), XML)
	if err != nil {
		t.Fatalf("xmlns attributes rejected: %v", err)
	}
	if !strings.Contains(out, `xmlns="urn:x"`) || !strings.Contains(out, `xmlns:p="urn:y"`) {
		t.Errorf("xmlns output = %s", out)
	}
	if xmlValidAttrName("a:b:c") || xmlValidAttrName(":") {
		t.Error("xmlValidAttrName accepted a malformed qualified name")
	}
}

// An empty element is an absent #text, i.e. the empty string — that is what
// makes "" and nil round-trip through XML.
func TestXMLEmptyElementIsEmptyString(t *testing.T) {
	out, err := Encode(om("root", om("a", "")), XML)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(out, XML)
	if err != nil {
		t.Fatal(err)
	}
	if !nodeEqual(back, om("root", om("a", "")), false) {
		t.Errorf(`empty string did not round-trip: %#v (encoded %q)`, back, out)
	}
}

// Lua source cannot carry an integer beyond int64 (a decimal literal that
// overflows is read back as a float), so the encoder rejects it instead of
// emitting digits that read back as a different value.
func TestLuaRejectsIntegersBeyondInt64(t *testing.T) {
	for name, n := range map[string]Node{
		"uint64 above MaxInt64": om("k", uint64(1)<<63),
		"big.Int 2^64":          om("k", new(big.Int).Lsh(big.NewInt(1), 64)),
		"big.Int 2^100":         om("k", new(big.Int).Lsh(big.NewInt(1), 100)),
		"nested in array":       om("k", arr(new(big.Int).Lsh(big.NewInt(1), 70))),
	} {
		if _, err := Encode(n, Lua); !errors.Is(err, ErrUnsupportedStructure) {
			t.Errorf("%s: err = %v, want ErrUnsupportedStructure", name, err)
		}
	}
	// Everything up to MaxInt64 is fine, including the boundary.
	for name, n := range map[string]Node{
		"MaxInt64":             om("k", int64(1)<<62),
		"uint64 within range":  om("k", uint64(7)),
		"big.Int within int64": om("k", big.NewInt(9007199254740993)),
	} {
		out, err := Encode(n, Lua)
		if err != nil {
			t.Errorf("%s rejected: %v", name, err)
			continue
		}
		back, err := Parse(out, Lua)
		if err != nil {
			t.Errorf("%s: encoded %q does not re-parse: %v", name, out, err)
			continue
		}
		if !nodeEqual(n, back, false) {
			t.Errorf("%s round-tripped to %#v (encoded %q)", name, back, out)
		}
	}
}

// goja degrades integers past int64 to float64; the literal text is exact, so
// the parser recovers it (Requirement 7.1).
func TestJSRecoversBigIntegersFromLiteralText(t *testing.T) {
	n, err := parseJS(`({a: 18446744073709551616, b: 1267650600228229401496703205376, c: 1.5, d: 1e2, e: 1.0, f: 9007199254740993})`)
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*OrderedMap)
	for _, tc := range []struct {
		key  string
		want Node
	}{
		{"a", new(big.Int).Lsh(big.NewInt(1), 64)},
		{"b", new(big.Int).Lsh(big.NewInt(1), 100)},
		{"c", 1.5},
		{"d", 100.0}, // exponent form is a float in JS, and stays one
		{"e", 1.0},   // so is an explicit ".0"
		{"f", int64(9007199254740993)},
	} {
		got, _ := m.Get(tc.key)
		if !nodeEqual(got, tc.want, true) {
			t.Errorf("%s = %T %#v, want %T %#v", tc.key, got, got, tc.want, tc.want)
		}
	}
	// Full round trip through the JS encoder.
	big2 := om("big", new(big.Int).Lsh(big.NewInt(1), 100))
	out, err := Encode(big2, JS)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(out, JS)
	if err != nil {
		t.Fatal(err)
	}
	if !nodeEqual(big2, back, true) {
		t.Errorf("big int did not round-trip through JS: %#v (encoded %s)", back, out)
	}
}

// goja hands back a unistring.String that holds raw UTF-16 code units once the
// literal leaves ASCII; converting it with string() corrupted every non-ASCII
// value and key, and the encoder then rejected its own parse result.
func TestJSNonASCIIStringsSurvive(t *testing.T) {
	for _, s := range []string{
		"héllo", "日本語", "🎉", "a🎉b", "Привет", "ÿ", "￿",
		"mixed ascii + 中文 + 🎉", "\U0001F600\U0001F601",
	} {
		n, err := parseJS(`({k: ` + strconv.Quote(s) + `})`)
		if err != nil {
			t.Errorf("parse %q: %v", s, err)
			continue
		}
		got, _ := n.(*OrderedMap).Get("k")
		if got != s {
			t.Errorf("value %q parsed as %q", s, got)
		}
		// The same corruption hit key position.
		kn, err := parseJS(`({` + strconv.Quote(s) + `: 1})`)
		if err != nil {
			t.Errorf("parse key %q: %v", s, err)
			continue
		}
		if v, ok := kn.(*OrderedMap).Get(s); !ok || v != int64(1) {
			t.Errorf("key %q -> %#v", s, kn)
		}
		// Full round trip: the encoder used to reject the invalid UTF-8.
		out, err := Encode(om("k", s), JS)
		if err != nil {
			t.Errorf("encode %q: %v", s, err)
			continue
		}
		back, err := Parse(out, JS)
		if err != nil {
			t.Errorf("encoded %q does not re-parse: %v", s, err)
			continue
		}
		if !nodeEqual(back, om("k", s), true) {
			t.Errorf("%q round-tripped to %#v", s, back)
		}
	}
	// A lone surrogate has no UTF-8 form; utf16.Decode would substitute U+FFFD.
	for name, input := range map[string]string{
		"lone high":     `({k: "\uD800"})`,
		"lone low":      `({k: "\uDC00"})`,
		"reversed pair": `({k: "\uDC00\uD800"})`,
		"high then eof": `({k: "a\uD83D"})`,
		"in key":        `({"\uD800": 1})`,
	} {
		if _, err := parseJS(input); err == nil {
			t.Errorf("%s: accepted %s", name, input)
		}
	}
	// A correctly paired surrogate is just an astral character and is fine.
	n, err := parseJS(`({k: "😀"})`)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := n.(*OrderedMap).Get("k"); v != "\U0001F600" {
		t.Errorf("surrogate pair -> %q", v)
	}
}

// A JS syntax error used to arrive with no position at all.
func TestJSSyntaxErrorCarriesPosition(t *testing.T) {
	var pe *ParseError
	if _, err := parseJS(`({a: })`); !errors.As(err, &pe) {
		t.Fatalf("err = %v (%T), want *ParseError", err, err)
	}
	if pe.Format != JS || pe.Line != 1 || pe.Column < 1 {
		t.Errorf("position = %s line %d col %d", pe.Format, pe.Line, pe.Column)
	}
	// The prepended "(" must not be charged to a later line's column.
	if _, err := parseJS("({\n  a: )\n})"); !errors.As(err, &pe) {
		t.Fatalf("err = %v (%T)", err, err)
	}
	if pe.Line != 2 || pe.Column < 1 {
		t.Errorf("multi-line position = line %d col %d", pe.Line, pe.Column)
	}
}

// Only the JS parser should say "js"; the shared negation helper used to
// attribute every unary-minus error to Lua.
func TestUnaryMinusErrorNamesItsOwnFormat(t *testing.T) {
	var pe *ParseError
	if _, err := parseJS(`({a: -"x"})`); !errors.As(err, &pe) || pe.Format != JS {
		t.Errorf("JS unary minus error = %v (format %v)", err, pe.Format)
	}
	if _, err := parseLua(`{a = -"x"}`); !errors.As(err, &pe) || pe.Format != Lua {
		t.Errorf("Lua unary minus error = %v (format %v)", err, pe.Format)
	}
}

// Lua's decimal \ddd escape consumes up to three digits, so a one-digit \0
// followed by a digit read back as a different byte.
func TestLuaNULEscapeIsThreeDigits(t *testing.T) {
	for _, s := range []string{
		"x\x00y", "x\x005", "\x00", "\x000", "\x009\x001",
		"a\x01" + "2", "\x1f9", "\x7f0",
	} {
		out, err := Encode(om("k", s), Lua)
		if err != nil {
			t.Errorf("Encode(%q): %v", s, err)
			continue
		}
		back, err := Parse(out, Lua)
		if err != nil {
			t.Errorf("encoded %q does not re-parse: %v (%q)", s, err, out)
			continue
		}
		if v, _ := back.(*OrderedMap).Get("k"); v != s {
			t.Errorf("%q round-tripped to %q (encoded %q)", s, v, out)
		}
	}
}

// Lua has one empty table constructor, and it reads back as a map — so an
// empty array cannot be encoded without silently changing shape.
func TestLuaRejectsEmptyArray(t *testing.T) {
	for name, n := range map[string]Node{
		"root":            arr(),
		"map value":       om("k", arr()),
		"nested in array": arr(arr(int64(1)), arr()),
	} {
		if _, err := Encode(n, Lua); !errors.Is(err, ErrUnsupportedStructure) {
			t.Errorf("%s: err = %v, want ErrUnsupportedStructure", name, err)
		}
	}
	// A non-empty array still round-trips, and an empty *map* is still fine.
	for name, n := range map[string]Node{
		"non-empty array": om("k", arr(int64(1))),
		"empty map":       om("k", NewOrderedMap()),
	} {
		out, err := Encode(n, Lua)
		if err != nil {
			t.Errorf("%s rejected: %v", name, err)
			continue
		}
		back, err := Parse(out, Lua)
		if err != nil {
			t.Errorf("%s: encoded %q does not re-parse: %v", name, out, err)
			continue
		}
		if !nodeEqual(n, back, false) {
			t.Errorf("%s round-tripped to %#v (encoded %q)", name, back, out)
		}
	}
}

// A Go string is UTF-8, so an escape naming a surrogate or a value past
// U+10FFFF has no representation; those used to become U+FFFD silently.
func TestPythonRejectsUnrepresentableEscapes(t *testing.T) {
	for name, input := range map[string]string{
		`\U surrogate low`:  `{"k": "\U0000D800"}`,
		`\U surrogate high`: `{"k": "\U0000DFFF"}`,
		`\U above max`:      `{"k": "\U00110000"}`,
		`\U far above max`:  `{"k": "\UFFFFFFFF"}`,
		`\u surrogate`:      `{"k": "\uD800"}`,
		`\u surrogate low`:  `{"k": "\uDFFF"}`,
		`in key`:            `{"\uD800": 1}`,
	} {
		if _, err := parsePython(input); err == nil {
			t.Errorf("%s: accepted %s", name, input)
		}
	}
	// Everything inside the representable range still decodes exactly.
	for input, want := range map[string]string{
		`{"k": "\U00000041"}`: "A",
		`{"k": "\U0010FFFF"}`: "\U0010FFFF",
		`{"k": "\U0001F600"}`: "\U0001F600",
		`{"k": "￿"}`:          "￿",
		`{"k": "퟿"}`:          "퟿",
		`{"k": ""}`:          "",
		`{"k": "\xff"}`:       "ÿ",
	} {
		n, err := parsePython(input)
		if err != nil {
			t.Errorf("%s: %v", input, err)
			continue
		}
		if v, _ := n.(*OrderedMap).Get("k"); v != want {
			t.Errorf("%s -> %q, want %q", input, v, want)
		}
	}
}

// ast.literal_eval("(1)") is the scalar 1: parentheses group, only a comma
// builds a tuple. The parser used to wrap every parenthesized value in a
// one-element array.
func TestPythonParenthesesAreNotAlwaysTuples(t *testing.T) {
	for input, want := range map[string]Node{
		`{"k": (1)}`:         int64(1),
		`{"k": ("s")}`:       "s",
		`{"k": (True)}`:      true,
		`{"k": (None)}`:      nil,
		`{"k": (-1.5)}`:      -1.5,
		`{"k": ((1))}`:       int64(1),
		`{"k": (1,)}`:        arr(int64(1)),
		`{"k": (1, 2)}`:      arr(int64(1), int64(2)),
		`{"k": (1, 2,)}`:     arr(int64(1), int64(2)),
		`{"k": ()}`:          arr(),
		`{"k": ([1])}`:       arr(int64(1)),
		`{"k": ((1,),)}`:     arr(arr(int64(1))),
		`{"k": (1, (2, 3))}`: arr(int64(1), arr(int64(2), int64(3))),
	} {
		n, err := parsePython(input)
		if err != nil {
			t.Errorf("%s: %v", input, err)
			continue
		}
		got, _ := n.(*OrderedMap).Get("k")
		if !nodeEqual(got, want, true) {
			t.Errorf("%s -> %T %#v, want %T %#v", input, got, got, want, want)
		}
	}
	// A parenthesized scalar at the root is a scalar, so it is still rejected.
	if _, err := parsePython(`(1)`); !errors.Is(err, ErrTopLevelScalar) {
		t.Errorf("root (1): err = %v, want ErrTopLevelScalar", err)
	}
	// ...but a real one-element tuple is a valid root.
	if n, err := parsePython(`(1,)`); err != nil {
		t.Errorf("root (1,): %v", err)
	} else if !nodeEqual(n, arr(int64(1)), true) {
		t.Errorf("root (1,) -> %#v", n)
	}
}

// Duplicate-key and positional-error fixes. These used to surface as bare
// sentinels (or position-less errors); they now carry the offending location
// while staying reachable via errors.Is.
func TestDuplicateKeyCarriesPosition(t *testing.T) {
	cases := []struct {
		name     string
		parse    func() error
		wantLine int
	}{
		{"json", func() error { _, err := parseJSON("{\"a\":1,\n\"a\":2}"); return err }, 2},
		{"yaml", func() error { _, err := parseYAML("a: 1\na: 2\n"); return err }, 2},
		{"python", func() error { _, err := parsePython("{\"a\": 1,\n\"a\": 2}"); return err }, 2},
	}
	for _, tc := range cases {
		var pe *ParseError
		err := tc.parse()
		if !errors.Is(err, ErrDuplicateKey) {
			t.Errorf("%s: not ErrDuplicateKey: %v", tc.name, err)
			continue
		}
		if !errors.As(err, &pe) {
			t.Errorf("%s: err is %T, want *ParseError wrapping the sentinel", tc.name, err)
			continue
		}
		if pe.Line != tc.wantLine {
			t.Errorf("%s: duplicate-key line = %d, want %d (%v)", tc.name, pe.Line, tc.wantLine, err)
		}
		if pe.Column < 1 {
			t.Errorf("%s: duplicate-key column = %d, want >= 1", tc.name, pe.Column)
		}
		if !strings.Contains(pe.Error(), "duplicate") {
			t.Errorf("%s: error text %q lacks 'duplicate'", tc.name, pe.Error())
		}
	}
}

func TestJSONParseErrorsCarryPosition(t *testing.T) {
	var pe *ParseError
	// Unterminated object: the syntax error points inside the document, not 0,0.
	_, err := parseJSON("{\"a\": ")
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v (%T)", err, err)
	}
	if pe.Format != JSON || pe.Line < 1 {
		t.Errorf("unterminated object: line=%d format=%s", pe.Line, pe.Format)
	}
	// A multi-line document reports the second line.
	if _, err := parseJSON("{\n  \"a\": ,\n}"); !errors.As(err, &pe) {
		t.Fatalf("err = %v (%T)", err, err)
	}
	if pe.Line != 2 {
		t.Errorf("bad token line = %d, want 2 (%v)", pe.Line, err)
	}
	// Trailing data after a complete document names the location of the junk.
	if _, err := parseJSON("{} garbage"); !errors.As(err, &pe) || pe.Column < 1 {
		t.Errorf("trailing data: %v (%T)", err, err)
	}
}

func TestUnsupportedFormatListsNames(t *testing.T) {
	_, perr := Parse("x", Format(999))
	if perr == nil || !strings.Contains(perr.Error(), "json") || !strings.Contains(perr.Error(), "lua") {
		t.Errorf("Parse unsupported format = %v, want a list including json and lua", perr)
	}
	if _, eerr := Encode(om(), Format(999)); eerr == nil || !strings.Contains(eerr.Error(), "toml") {
		t.Errorf("Encode unsupported format = %v, want a list including toml", eerr)
	}
}

// The array-of-tables cursor key used to be built by concatenating "\x00"+seg
// and "#"+index, which is not injective: element 0 of a real [[a]] array and an
// ordinary quoted key "a#0" both encoded to "\x00a#0". The two unrelated nodes
// then shared one counter, silently dropping leaf values and growing phantom
// empty tables. No exotic input is required — "issue#0" is ordinary TOML.
func TestTOMLCursorKeyIsInjective(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  Node
	}{
		{
			"quoted key collides with element 0",
			"[[a]]\n[[a.b]]\nx = 1\n\n[\"a#0\"]\n[[\"a#0\".b]]\ny = 1\n",
			om("a", arr(om("b", arr(om("x", int64(1))))),
				"a#0", om("b", arr(om("y", int64(1))))),
		},
		{
			"collision with two elements",
			"[[a]]\n[[a.b]]\nx = 1\n\n[\"a#0\"]\n[[\"a#0\".b]]\ny = 1\n[[\"a#0\".b]]\ny = 2\n",
			om("a", arr(om("b", arr(om("x", int64(1))))),
				"a#0", om("b", arr(om("y", int64(1)), om("y", int64(2))))),
		},
		{
			"realistic issue#0 key",
			"[[issue]]\n[[issue.tag]]\nn = \"p\"\n\n[\"issue#0\"]\n[[\"issue#0\".tag]]\nn = \"q\"\n",
			om("issue", arr(om("tag", arr(om("n", "p")))),
				"issue#0", om("tag", arr(om("n", "q")))),
		},
		{
			// Reversed source order: the damage used to land on the genuine array.
			"reversed order",
			"[\"a#0\"]\n[[\"a#0\".b]]\ny = 1\n\n[[a]]\n[[a.b]]\nx = 1\n",
			om("a#0", om("b", arr(om("y", int64(1)))),
				"a", arr(om("b", arr(om("x", int64(1)))))),
		},
	} {
		got, err := parseTOML(tc.input)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if !nodeEqual(got, tc.want, false) {
			t.Errorf("%s:\n got  %#v\n want %#v", tc.name, got, tc.want)
		}
	}
	// The encoding itself must not let a segment merge with an index.
	if tomlCursorSeg("a")+tomlCursorIdx(0) == tomlCursorSeg("a#0") {
		t.Error("cursor encoding is not injective: seg+idx aliases a literal key")
	}
	if tomlCursorSeg("a")+tomlCursorSeg("b") == tomlCursorSeg("a\x00b") {
		t.Error("cursor encoding is not injective: two segments alias a NUL key")
	}
	if tomlCursorSeg("a1")+tomlCursorIdx(2) == tomlCursorSeg("a")+tomlCursorIdx(12) {
		t.Error("cursor encoding is not injective: digits merge with the index")
	}
}

// Indentation is whitespace inside the element, so for mixed content it used to
// be folded into #text on the way back in: {"#text":"hello"} re-parsed as
// {"#text":"hello\n"}. Such documents are now emitted unindented.
func TestXMLMixedContentRoundTrips(t *testing.T) {
	for _, want := range []*OrderedMap{
		om("root", om("#text", "hello", "child", "world")),
		om("root", om("child", om("#text", "a", "g", "b"))),
		om("root", om("@id", "7", "#text", "hello", "child", "world")),
		om("root", om("list", arr(om("#text", "x", "e", "y"), om("#text", "z", "e", "w")))),
	} {
		out, err := Encode(want, XML)
		if err != nil {
			t.Errorf("encode %#v: %v", want, err)
			continue
		}
		got, err := Parse(out, XML)
		if err != nil {
			t.Errorf("reparse %q: %v", out, err)
			continue
		}
		if !nodeEqual(got, want, true) {
			t.Errorf("mixed content did not round-trip\n encoded %q\n got     %#v\n want    %#v", out, got, want)
		}
	}
	// Documents without mixed content stay indented.
	out, err := Encode(om("root", om("a", "1")), XML)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(out, "\n  <a>") {
		t.Errorf("non-mixed document lost its indentation: %q", out)
	}
}

// Parse used to return bare sentinels for the whole-document rejections, so the
// caller could not tell which format had failed. They are now *ParseError with
// Format set, and errors.Is still reaches the sentinel.
func TestParseSentinelsCarryFormat(t *testing.T) {
	deep := strings.Repeat("[", maxDepth+1) + strings.Repeat("]", maxDepth+1)
	for _, tc := range []struct {
		name  string
		f     Format
		input string
		want  error
	}{
		{"json scalar", JSON, "42", ErrTopLevelScalar},
		{"yaml scalar", YAML, "42", ErrTopLevelScalar},
		{"lua scalar", Lua, "42", ErrTopLevelScalar},
		{"python scalar", Python, "42", ErrTopLevelScalar},
		{"js scalar", JS, "42", ErrTopLevelScalar},
		{"json deep", JSON, deep, ErrTooDeep},
		{"yaml deep", YAML, deep, ErrTooDeep},
		{"python deep", Python, deep, ErrTooDeep},
	} {
		_, err := Parse(tc.input, tc.f)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: errors.Is(%v, %v) = false", tc.name, err, tc.want)
			continue
		}
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Errorf("%s: want a *ParseError, got %T (%v)", tc.name, err, err)
			continue
		}
		if pe.Format != tc.f {
			t.Errorf("%s: Format = %v, want %v", tc.name, pe.Format, tc.f)
		}
		if strings.Contains(pe.Msg, "structure: ") {
			t.Errorf("%s: message keeps the redundant package prefix: %q", tc.name, pe.Msg)
		}
	}
}
