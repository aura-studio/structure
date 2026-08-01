package structure

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"
)

// This file closes the remaining branch gaps: parser error paths that need a
// very specific input, and the defensive "invalid node type" arms of the
// internal writers, which Encode's validate() pass makes unreachable from the
// public API and so are exercised by calling the writers directly.

// badNode is not a legal Node carrier; the writers must reject it.
var badNode Node = int(1)

func TestJSParserErrorPaths(t *testing.T) {
	for name, input := range map[string]string{
		"syntax":            `{`,
		"two expressions":   `1);(2`,
		"array element":     `({a: [b]})`,
		"nested unary":      `({a: -(-b)})`,
		"unary non-numeric": `({a: -"s"})`,
	} {
		if _, err := parseJS(input); err == nil {
			t.Errorf("%s: accepted %q", name, input)
		}
	}
	// Reserved words are valid property keys (goja lowers them to string keys).
	n, err := parseJS(`({null: 1, true: 2, class: 3})`)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"null", "true", "class"} {
		if _, ok := n.(*OrderedMap).Get(k); !ok {
			t.Errorf("missing key %q", k)
		}
	}
}

func TestJSONErrorAndWriterPaths(t *testing.T) {
	for name, input := range map[string]string{
		"garbage first token": `@`,
		"trailing garbage":    `{} }`,
		"trailing value":      `{} 1`,
	} {
		if _, err := parseJSON(input); err == nil {
			t.Errorf("%s: accepted %q", name, input)
		}
	}
	// jsonTokenName covers every token kind it can be handed.
	for tok, want := range map[json.Token]string{
		json.Delim('{'):     `delimiter "{"`,
		json.Number("1.5"):  "number 1.5",
		json.Token("s"):     "string",
		json.Token(true):    "boolean",
		json.Token(nil):     "null",
		json.Token(int(42)): "int",
	} {
		if got := jsonTokenName(tok); got != want {
			t.Errorf("jsonTokenName(%v) = %q, want %q", tok, got, want)
		}
	}
	// value rejects a stray closing delimiter and an unknown token type. A
	// parser with no decoder reports position 0,0 rather than panicking.
	jp := &jsonParser{}
	if _, err := jp.value(json.Delim('}'), 1); err == nil {
		t.Error("jsonParser.value accepted a closing delimiter")
	}
	if _, err := jp.value(json.Token(int(1)), 1); err == nil {
		t.Error("jsonParser.value accepted an unknown token")
	}
	// wrapJSONErr maps the standard library's depth message onto ErrTooDeep.
	if err := wrapJSONErr(errors.New("exceeded max depth")); !errors.Is(err, ErrTooDeep) {
		t.Errorf("wrapJSONErr(depth) = %v, want ErrTooDeep", err)
	}
	// Empty containers at the root, and the writers' invalid-type arms.
	for _, tc := range []struct {
		n    Node
		want string
	}{{om(), "{}\n"}, {arr(), "[]\n"}} {
		if out, err := encodeJSON(tc.n); err != nil || out != tc.want {
			t.Errorf("encodeJSON(%#v) = %q, %v", tc.n, out, err)
		}
	}
	var b strings.Builder
	if err := writeJSONValue(&b, badNode, 0); err == nil {
		t.Error("writeJSONValue accepted an illegal carrier")
	}
	if err := writeJSONArray(&b, arr(badNode), 0); err == nil {
		t.Error("writeJSONArray accepted an illegal element")
	}
	if err := writeJSONObject(&b, om("k", badNode), 0); err == nil {
		t.Error("writeJSONObject accepted an illegal value")
	}
	if _, err := jsonMarshalNode(arr(badNode)); err == nil {
		t.Error("jsonMarshalNode accepted an illegal element")
	}
}

func TestLuaErrorAndWriterPaths(t *testing.T) {
	for name, input := range map[string]string{
		"hash then array":      `{a = 1, 2}`,
		"array element error":  `{ -"x" }`,
		"nested unary operand": `{a = - -"x"}`,
	} {
		if _, err := parseLua(input); err == nil {
			t.Errorf("%s: accepted %q", name, input)
		}
	}
	// negateNumber over the *big.Int carrier.
	huge := new(big.Int).Lsh(big.NewInt(1), 100)
	neg, err := negateNumber(Lua, huge)
	if err != nil {
		t.Fatal(err)
	}
	if neg.(*big.Int).Sign() != -1 {
		t.Errorf("negateNumber(2^100) = %v", neg)
	}
	if _, err := negateNumber(Lua, new(big.Int).SetUint64(math.MaxUint64)); err != nil {
		t.Fatal(err)
	}
	// wrapLuaErr falls back to a bare *ParseError for untyped errors.
	var pe *ParseError
	if err := wrapLuaErr(errors.New("boom")); !errors.As(err, &pe) || pe.Format != Lua {
		t.Errorf("wrapLuaErr = %v (%T)", err, err)
	}
	// Bare keys may contain digits after the first rune.
	out, err := encodeLua(om("a1", int64(1), "1a", int64(2)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a1 = 1") || !strings.Contains(out, `["1a"] = 2`) {
		t.Errorf("bare-key handling: %s", out)
	}
	var b strings.Builder
	if err := writeLuaValue(&b, nil, 0, false); err != nil {
		t.Fatalf("nil in hash position: %v", err)
	}
	if !strings.Contains(b.String(), "nil") {
		t.Errorf("nil rendered as %q", b.String())
	}
	if err := writeLuaValue(&b, badNode, 0, false); err == nil {
		t.Error("writeLuaValue accepted an illegal carrier")
	}
}

func TestPythonRemainingBranches(t *testing.T) {
	for name, input := range map[string]string{
		"trailing data":      `{} junk`,
		"unexpected char":    `{"a": ?}`,
		"non-string key":     `{1: 2}`,
		"unterminated slash": `{"a": "x\`,
		"truncated hex":      `{"a": "\x`,
		"signed hex escape":  `{"a": "\x+1"}`,
		"bytes prefix":       `{"a": rub'x'}`,
		"prefix overrun":     `{"a": rubr'x'}`,
		"bare name":          `{"a": rubbish}`,
		"f-string":           `{"a": f"x"}`,
		"underscore in dot":  `{"a": 1._5}`,
	} {
		if _, err := parsePython(input); err == nil {
			t.Errorf("%s: accepted %q", name, input)
		}
	}
	if err := pyValidateNumberLit("", 10); err == nil {
		t.Error("pyValidateNumberLit accepted an empty literal")
	}
	if lowerASCII('R') != 'r' || lowerASCII('r') != 'r' {
		t.Error("lowerASCII mishandled case")
	}

	// Accepted forms: trailing commas, uppercase raw prefix, triple quotes with
	// an embedded quote, \0 followed by an octal digit, signed exponents,
	// negative floats and False.
	n, err := parsePython(strings.Join([]string{
		`{"d": {"a": 1,},`,
		`"l": [1,],`,
		`"t": (1,),`,
		`"raw": R"a\nb",`,
		`"triple": '''a'b''',`,
		`"oct": "\01",`,
		`"exp": 1e+2,`,
		`"negexp": 1e-2,`,
		`"negf": -1.5,`,
		`"no": False}`,
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*OrderedMap)
	for k, want := range map[string]Node{
		"raw":    `a\nb`,
		"triple": "a'b",
		"oct":    string(rune(1)),
		"exp":    100.0,
		"negexp": 0.01,
		"negf":   -1.5,
		"no":     false,
	} {
		if v, _ := m.Get(k); v != want {
			t.Errorf("%s = %#v, want %#v", k, v, want)
		}
	}

	// False renders as a Python literal.
	if out, err := encodePython(om("f", false)); err != nil || !strings.Contains(out, "False") {
		t.Errorf("encodePython(false) = %q, %v", out, err)
	}
	var b strings.Builder
	if err := writePythonValue(&b, badNode, 0); err == nil {
		t.Error("writePythonValue accepted an illegal carrier")
	}
}

func TestTOMLRemainingBranches(t *testing.T) {
	// A mixed array (tables plus scalars) is not an array of tables, so it goes
	// out inline and exercises the inline-table writer.
	out, err := encodeTOML(om("mixed", arr(om("a", int64(1)), int64(2)), "empty_inline", arr(om())))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "mixed = [{a = 1}, 2]") {
		t.Errorf("inline table: %s", out)
	}
	back, err := parseTOML(out)
	if err != nil || !nodeEqual(back, om("mixed", arr(om("a", int64(1)), int64(2)), "empty_inline", arr(om())), false) {
		t.Fatalf("inline round trip: %v\n%s", err, out)
	}

	// In-range uint64 and *big.Int carriers, and the full escape table.
	esc := "a\bb\fc\rd\te" + string(rune(1))
	out, err = encodeTOML(om("u", uint64(7), "big", big.NewInt(8), "esc", esc))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"u = 7", "big = 8", `\b`, `\f`, `\r`, `\t`, `\u0001`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if back, err := parseTOML(out); err != nil {
		t.Fatal(err)
	} else if v, _ := back.(*OrderedMap).Get("esc"); v != esc {
		t.Errorf("escapes round-tripped to %q", v)
	}
	if _, err := encodeTOML(om("big", (*big.Int)(nil))); err == nil {
		t.Error("nil *big.Int encoded")
	}

	// A table nested inside an array-of-tables element can still fail.
	if _, err := encodeTOML(om("items", arr(om("bad", badNode)))); err == nil {
		t.Error("illegal carrier inside an array of tables encoded")
	}
	var b strings.Builder
	if err := writeTOMLValue(&b, badNode); err == nil {
		t.Error("writeTOMLValue accepted an illegal carrier")
	}
	if err := writeTOMLInlineTable(&b, om("k", badNode)); err == nil {
		t.Error("writeTOMLInlineTable accepted an illegal value")
	}

	// wrapTOMLErr falls back to a bare *ParseError for untyped errors.
	var pe *ParseError
	if err := wrapTOMLErr(errors.New("boom")); !errors.As(err, &pe) || pe.Format != TOML {
		t.Errorf("wrapTOMLErr = %v (%T)", err, err)
	}

	// tomlAnyToNode handles every decoded carrier, including the containers
	// BurntSushi only produces for inline values.
	got, err := tomlAnyToNode([]any{
		nil, true, "s", int64(1), 2.5,
		map[string]any{"b": int64(1), "a": int64(2)},
		[]map[string]any{{"x": int64(1)}},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	items := got.([]Node)
	if len(items) != 7 {
		t.Fatalf("tomlAnyToNode = %#v", got)
	}
	if keys := items[5].(*OrderedMap).Keys(); strings.Join(keys, ",") != "a,b" {
		t.Errorf("inline table keys = %v, want sorted", keys)
	}
	if inner, _ := items[6].([]Node)[0].(*OrderedMap).Get("x"); inner != int64(1) {
		t.Errorf("[]map[string]any = %#v", items[6])
	}
	if _, err := tomlAnyToNode(int32(1), 1); err == nil {
		t.Error("tomlAnyToNode accepted an unknown decoded type")
	}
	if _, err := tomlAnyToNode(map[string]any{"a": int64(1)}, maxDepth+1); !errors.Is(err, ErrTooDeep) {
		t.Error("tomlAnyToNode ignored the depth guard")
	}
	if _, err := tomlAnyToNode([]any{[]any{int64(1)}}, maxDepth); !errors.Is(err, ErrTooDeep) {
		t.Error("nested tomlAnyToNode ignored the depth guard")
	}
	if _, err := tomlAnyToNode([]map[string]any{{"a": map[string]any{"b": int64(1)}}}, maxDepth); !errors.Is(err, ErrTooDeep) {
		t.Error("array-of-tables tomlAnyToNode ignored the depth guard")
	}
}

func TestTOMLReplayConflicts(t *testing.T) {
	// Deeply-dotted keys, sub-tables created implicitly, and arrays of tables
	// addressed through a parent header all flow through resolve().
	n, err := parseTOML(strings.Join([]string{
		`a.b.c = 1`,
		`[t]`,
		`u.v = 2`,
		`[[g]]`,
		`x = 1`,
		`[g.h]`,
		`y = 2`,
		`[[g.k]]`,
		`z = 3`,
		`[[g]]`,
		`x = 4`,
	}, "\n") + "\n")
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*OrderedMap)
	ab, _ := m.Get("a")
	b, _ := ab.(*OrderedMap).Get("b")
	if c, _ := b.(*OrderedMap).Get("c"); c != int64(1) {
		t.Errorf("a.b.c = %v", c)
	}
	g, _ := m.Get("g")
	gs := g.([]Node)
	if len(gs) != 2 {
		t.Fatalf("g = %#v", gs)
	}
	if _, ok := gs[1].(*OrderedMap).Get("h"); ok {
		t.Error("g[0].h leaked into g[1]")
	}

	// Every parse error the replay itself can raise still surfaces as an error.
	for _, input := range []string{
		"[[a]]\nx = 1\n[a.b]\ny = 2\n[a.b]\nz = 3\n",
		"a = 1\n[a.b]\nc = 2\n",
		"[[a]]\n[[a.b]]\nx = 1\n[a.b]\ny = 2\n",
	} {
		if _, err := parseTOML(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

func TestXMLRemainingBranches(t *testing.T) {
	if line, col := xmlLineCol("abc", -1); line != 0 || col != 0 {
		t.Errorf("xmlLineCol(-1) = %d,%d", line, col)
	}
	if line, col := xmlLineCol("a\nb", 999); line != 2 || col != 2 {
		t.Errorf("xmlLineCol(past end) = %d,%d", line, col)
	}
	// #text must be a scalar, in a plain element and inside an array.
	if _, err := encodeXML(om("root", om("#text", arr(int64(1))))); !errors.Is(err, ErrUnsupportedStructure) {
		t.Error("container #text encoded")
	}
	if _, err := encodeXML(om("root", om("list", arr(om("bad", math.NaN()))))); !errors.Is(err, ErrUnsupportedStructure) {
		t.Error("NaN inside a same-name array encoded")
	}
	if _, err := encodeXML(om("root", om("child", badNode))); err == nil {
		t.Error("illegal carrier encoded to XML")
	}
	if _, err := xmlScalarString(om()); !errors.Is(err, ErrUnsupportedStructure) {
		t.Error("xmlScalarString accepted a map")
	}
}

func TestYAMLRemainingErrorPaths(t *testing.T) {
	for name, input := range map[string]string{
		"broken second document": "a: 1\n---\n[1,\n",
		"tagged sequence":        "a: !foo [1]\n",
		"sequence element":       "a: [!!binary x]\n",
		"merge key":              "<<: {a: 1}\nb: 2\n",
		"custom scalar tag":      "a: !custom x\n",
		"tagged mapping":         "a: !foo {b: 1}\n",
	} {
		if _, err := parseYAML(input); err == nil {
			t.Errorf("%s: accepted %q", name, input)
		}
	}
	// A bare document marker resolves to null, which is not a container root.
	if _, err := parseYAML("---"); !errors.Is(err, ErrTopLevelScalar) {
		t.Error("--- accepted as a document")
	}
	var pe *ParseError
	if err := wrapYAMLErr(errors.New("boom")); !errors.As(err, &pe) || pe.Format != YAML {
		t.Errorf("wrapYAMLErr = %v (%T)", err, err)
	}
	// nodeToYAML's defensive arms.
	if _, err := nodeToYAML((*big.Int)(nil)); err == nil {
		t.Error("nil *big.Int converted")
	}
	if _, err := nodeToYAML(badNode); err == nil {
		t.Error("illegal carrier converted")
	}
	if _, err := nodeToYAML(om("k", badNode)); err == nil {
		t.Error("illegal map value converted")
	}
	if _, err := nodeToYAML(arr(badNode)); err == nil {
		t.Error("illegal array element converted")
	}
}

func TestNodeAndOrderedMapRemainingBranches(t *testing.T) {
	// nodeClone passes typed nil pointers through untouched.
	if got := nodeClone((*OrderedMap)(nil)); got.(*OrderedMap) != nil {
		t.Error("nil *OrderedMap cloned into something")
	}
	if got := nodeClone((*big.Int)(nil)); got.(*big.Int) != nil {
		t.Error("nil *big.Int cloned into something")
	}
	// nodeEqual: carrier mismatches, nested inequality, illegal types.
	for i, c := range []struct {
		a, b Node
		want bool
	}{
		{1.5, "x", false},
		{om("a", om("b", int64(1))), om("a", om("b", int64(2))), false},
		{badNode, badNode, false},
		{(*big.Int)(nil), int64(0), false},
	} {
		if got := nodeEqual(c.a, c.b, true); got != c.want {
			t.Errorf("case %d: nodeEqual = %v, want %v", i, got, c.want)
		}
	}
	if _, ok := asBigInt((*big.Int)(nil)); ok {
		t.Error("asBigInt accepted a nil pointer")
	}
	if _, err := formatNumber((*big.Int)(nil)); err == nil {
		t.Error("formatNumber accepted a nil pointer")
	}
	// jsonEscape covers the whole escape table.
	got := jsonEscape("a\rb\bc\fd")
	for _, want := range []string{`\r`, `\b`, `\f`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	// A zero-value OrderedMap lazily builds its index on first Set.
	var zero OrderedMap
	zero.Set("a", int64(1))
	if v, ok := zero.Get("a"); !ok || v != int64(1) {
		t.Errorf("zero-value Set/Get = %v, %v", v, ok)
	}
}

func TestInvalidUTF8IsRejectedAtBothBoundaries(t *testing.T) {
	// A stray continuation byte. The byte-oriented parsers (Python, Lua) used to
	// carry it into a string, and the rune-ranging encoders then substituted
	// U+FFFD — a silent round-trip corruption found by FuzzRoundTrip.
	stray := string([]byte{0x84})

	for _, f := range allFormats {
		var pe *ParseError
		if _, err := Parse(`{"`+stray+`":[]}`, f); !errors.As(err, &pe) || pe.Format != f {
			t.Errorf("Parse(%s) of invalid UTF-8 = %v (%T), want a *ParseError", f, err, err)
		}
	}
	// The offset is reported as a position, not swallowed.
	var pe *ParseError
	if _, err := Parse("{\n\""+stray+"\":1}", JSON); errors.As(err, &pe) {
		if pe.Line != 2 || pe.Column != 2 {
			t.Errorf("invalid UTF-8 reported at line %d, col %d, want 2,2", pe.Line, pe.Column)
		}
	}
	// Valid UTF-8 that merely looks exotic still parses.
	if _, err := Parse(`{"héllo":"日本語 🎉"}`, JSON); err != nil {
		t.Errorf("multibyte UTF-8 rejected: %v", err)
	}

	// Encode rejects stray bytes in both values and keys, for every target.
	for _, f := range allFormats {
		if _, err := Encode(om("root", om("k", stray)), f); err == nil {
			t.Errorf("Encode(%s) accepted an invalid UTF-8 value", f)
		}
		if _, err := Encode(om("root", om(stray, "v")), f); err == nil {
			t.Errorf("Encode(%s) accepted an invalid UTF-8 key", f)
		}
	}
	if err := validate(arr(stray)); err == nil {
		t.Error("validate accepted an invalid UTF-8 array element")
	}
	if off := firstInvalidUTF8("ok"); off != -1 {
		t.Errorf("firstInvalidUTF8(valid) = %d, want -1", off)
	}
	// A truncated multi-byte prefix is invalid but decodes with size > 1 only
	// for genuine runes; the scan must still land on the offending offset.
	if got := firstInvalidUTF8("ab" + string([]byte{0xe4, 0xb8})); got != 2 {
		t.Errorf("firstInvalidUTF8(truncated) = %d, want 2", got)
	}
}

func TestParserDepthGuardsAtTheLimit(t *testing.T) {
	for _, tc := range []struct {
		name  string
		parse func(string) (Node, error)
		input string
	}{
		{"json object", parseJSON, strings.Repeat(`{"a":`, maxDepth+1) + "1" + strings.Repeat("}", maxDepth+1)},
		{"lua", parseLua, deepLua(maxDepth + 1)},
		{"python", parsePython, deepPython(maxDepth + 1)},
		{"js", parseJS, deepJS(maxDepth + 1)},
		{"toml", parseTOML, deepTOML(maxDepth + 1)},
	} {
		if _, err := tc.parse(tc.input); !errors.Is(err, ErrTooDeep) {
			t.Errorf("%s at %d levels: err = %v, want ErrTooDeep", tc.name, maxDepth+1, err)
		}
	}
	// YAML enforces its own limit before ours; it must still be an error.
	if _, err := parseYAML(deepYAML(maxDepth + 1)); err == nil {
		t.Error("YAML accepted 10001 levels")
	}
}
