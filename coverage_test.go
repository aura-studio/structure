package structure

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"
)

// This file targets the error branches and scalar-carrier edges that the
// behavioural tests do not reach on their own.

func TestJSONTokenNamesAndScalarRoots(t *testing.T) {
	for _, input := range []string{"1", "true", "null", `"x"`, "1.5"} {
		_, err := parseJSON(input)
		if !errors.Is(err, ErrTopLevelScalar) {
			t.Errorf("parseJSON(%q) err = %v, want ErrTopLevelScalar", input, err)
		}
	}
	for _, input := range []string{`{"a":}`, `{"a":1,}`, `[1,]`, `{1:2}`, `{"a" 1}`, `[`, `{"a":[1`} {
		if _, err := parseJSON(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	// A number too large for float64 still reports a parse error.
	if _, err := parseJSON(`{"a":1e400}`); err == nil {
		t.Error("1e400 accepted")
	}
}

func TestJSONMarshalNodeAllCarriers(t *testing.T) {
	m := om(
		"nil", nil,
		"bool", false,
		"int", int64(-3),
		"uint", uint64(math.MaxUint64),
		"big", new(big.Int).Lsh(big.NewInt(1), 70),
		"float", 2.5,
		"whole", 4.0,
		"str", "s",
		"arr", arr(int64(1), om("k", "v"), arr()),
		"empty", om(),
	)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"nil":null`, `"bool":false`, `"int":-3`,
		`"uint":18446744073709551615`, `"big":1180591620717411303424`,
		`"float":2.5`, `"whole":4.0`, `"arr":[1,{"k":"v"},[]]`, `"empty":{}`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s in %s", want, b)
		}
	}
	// NaN has no JSON form, even through the Marshaler hook.
	if _, err := json.Marshal(om("n", math.NaN())); err == nil {
		t.Error("NaN marshalled")
	}
	if _, err := jsonMarshalNode(int(1)); err == nil {
		t.Error("illegal node type marshalled")
	}
	var nilMap *OrderedMap
	if _, err := jsonMarshalNode(nilMap); err == nil {
		t.Error("nil *OrderedMap marshalled")
	}
}

func TestNodeHelpersEdges(t *testing.T) {
	if !isNumeric(uint64(1)) || !isNumeric(big.NewInt(1)) || !isNumeric(1.5) || !isNumeric(int64(1)) {
		t.Error("isNumeric rejected a numeric carrier")
	}
	if isNumeric("1") || isNumeric(nil) || isNumeric(true) {
		t.Error("isNumeric accepted a non-numeric value")
	}
	if got, err := formatNumber(uint64(math.MaxUint64)); err != nil || got != "18446744073709551615" {
		t.Errorf("formatNumber(uint64) = %q, %v", got, err)
	}
	if _, err := formatNumber(math.NaN()); !errors.Is(err, ErrUnsupportedStructure) {
		t.Error("formatNumber accepted NaN")
	}

	// nodeClone covers every carrier, including nested containers.
	src := om("a", arr(int64(1), "s", nil, true, 1.5, uint64(2), big.NewInt(3)), "m", om("x", int64(1)))
	clone := nodeClone(src).(*OrderedMap)
	if !nodeEqual(src, clone, true) {
		t.Fatal("clone not equal to source")
	}
	cloneArr, _ := clone.Get("a")
	cloneArr.([]Node)[0] = int64(99)
	srcArr, _ := src.Get("a")
	if srcArr.([]Node)[0] != int64(1) {
		t.Fatal("clone shares the array with the source")
	}

	// Cross-carrier numeric comparisons and mismatched container shapes.
	cases := []struct {
		a, b Node
		want bool
	}{
		{uint64(math.MaxUint64), new(big.Int).SetUint64(math.MaxUint64), true},
		{big.NewInt(-1), int64(-1), true},
		{uint64(1), 1.0, false},
		{arr(int64(1)), arr(int64(1), int64(2)), false},
		{om("a", int64(1)), om("a", int64(1), "b", int64(2)), false},
		{om("a", int64(1)), om("b", int64(1)), false},
		{"x", int64(1), false},
		{true, int64(1), false},
	}
	for i, c := range cases {
		if got := nodeEqual(c.a, c.b, true); got != c.want {
			t.Errorf("case %d: nodeEqual(%v, %v) = %v, want %v", i, c.a, c.b, got, c.want)
		}
	}
	if nodeEqual(om("a", int64(1)), om("b", int64(1)), false) {
		t.Error("unordered compare ignored key names")
	}
}

func TestLuaErrorsAndScalarEdges(t *testing.T) {
	// wrapLuaErr: golua reports a positioned parse error.
	var pe *ParseError
	_, err := parseLua("{1,")
	if !errors.As(err, &pe) || pe.Format != Lua {
		t.Fatalf("parseLua syntax error = %v (%T)", err, err)
	}

	// negateNumber across carriers, plus a non-numeric operand.
	n, err := parseLua(`{ i = -1, f = -1.5, big = -9223372036854775808, z = -0.0 }`)
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*OrderedMap)
	if v, _ := m.Get("i"); v != int64(-1) {
		t.Errorf("i = %v (%T)", v, v)
	}
	if v, _ := m.Get("f"); v != -1.5 {
		t.Errorf("f = %v", v)
	}
	if v, _ := m.Get("big"); v != int64(math.MinInt64) {
		t.Errorf("big = %v (%T)", v, v)
	}
	if _, err := negateNumber(Lua, "x"); err == nil {
		t.Error("negateNumber accepted a string")
	}
	// The error names the format doing the parsing, not always Lua.
	var npe *ParseError
	if _, err := negateNumber(JS, "x"); !errors.As(err, &npe) || npe.Format != JS {
		t.Errorf("negateNumber(JS, string) = %v", err)
	}
	if got := luaBigResult(new(big.Int).SetUint64(math.MaxUint64)); got != uint64(math.MaxUint64) {
		t.Errorf("luaBigResult(2^64-1) = %v (%T)", got, got)
	}
	huge := new(big.Int).Lsh(big.NewInt(1), 100)
	if got := luaBigResult(huge); got != huge {
		t.Errorf("luaBigResult(2^100) = %v (%T)", got, got)
	}

	// Rejected expression forms.
	for _, input := range []string{`{a = not true}`, `{a = #x}`, `{a = 1 + 1}`, `"x"`, `nil`, `{[true]=1}`} {
		if _, err := parseLua(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}

	// luaString escapes: control characters use decimal \ddd.
	out, err := encodeLua(om("k", "a\"b\\c\nd\re\tf"+string(rune(0))+string(rune(1))+string(rune(0x7f))))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`\"`, `\\`, `\n`, `\r`, `\t`, `\0`, `\001`, `\127`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if _, err := encodeLua(om("m", om("n", math.Inf(-1)))); !errors.Is(err, ErrUnsupportedStructure) {
		t.Error("nested -Inf encoded")
	}
	if _, err := encodeLua(arr(arr(nil))); !errors.Is(err, ErrUnsupportedStructure) {
		t.Error("nested nil array element encoded")
	}
	// A map whose only entries are nil collapses to an empty table.
	if out, err := encodeLua(om("gone", nil)); err != nil || !strings.Contains(out, "{}") {
		t.Fatalf("nil-only map = %q, %v", out, err)
	}
	// An empty array has no Lua spelling: {} reads back as an empty map, so the
	// encoder rejects it rather than silently changing the value's shape.
	if _, err := encodeLua(arr()); !errors.Is(err, ErrUnsupportedStructure) {
		t.Errorf("empty array encoded: %v", err)
	}
}

func TestPythonEscapesAndNumberEdges(t *testing.T) {
	input := `{"esc": "\a\b\f\v\r\n\t\\\'\"\0", "oct": "\101\377", "hex": "\x41", "u": "A", "U": "\U00000041"}`
	n, err := parsePython(input)
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*OrderedMap)
	esc, _ := m.Get("esc")
	want := "\a\b\f\v\r\n\t\\'\"" + string(rune(0))
	if esc != want {
		t.Errorf("esc = %q, want %q", esc, want)
	}
	if v, _ := m.Get("oct"); v != "A"+string(rune(0xff)) {
		t.Errorf("oct = %q", v)
	}
	for _, k := range []string{"hex", "u", "U"} {
		if v, _ := m.Get(k); v != "A" {
			t.Errorf("%s = %q, want A", k, v)
		}
	}

	// Line continuation drops the newline.
	cont, err := parsePython("{\"a\": \"x\\\ny\"}")
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := cont.(*OrderedMap).Get("a"); v != "xy" {
		t.Errorf("line continuation = %q, want %q", v, "xy")
	}

	// Comments and surrounding whitespace are skipped.
	if _, err := parsePython("# lead\n{ \"a\": 1, # trailing\n \"b\": 2 }\n# end\n"); err != nil {
		t.Fatal(err)
	}

	for _, bad := range []string{
		`{"a": "\q"}`, `{"a": "\x4"}`, `{"a": "\xZZ"}`, `{"a": "\u00"}`,
		`{"a": "\400"}`, `{"a": "unterminated}`, "{\"a\": \"nl\nnl\"}",
		`{"a": 1`, `{"a" 1}`, `{"a": 1 "b": 2}`, `[1 2]`, `(1 2)`, `{`, `[`, `(`,
		`{"a": .}`, `{"a": +}`, `{"a": -}`, `{"a": 0x}`, `"x" "y"`, `{"a": inf}`,
		`{"a": nan}`, `{"a": 1_}`, `{"a": _1}`, `{"a": 0b12}`, `{"a": u"x" "y"}`,
	} {
		if _, err := parsePython(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}

	// Underscore separators are legal between digits in every base.
	ok, err := parsePython(`{"d": 1_000, "h": 0x_1f, "o": 0o1_7, "b": 0b1_1, "f": 1_0.5e1_0, "neg": -0x10, "big": -18446744073709551617}`)
	if err != nil {
		t.Fatal(err)
	}
	okm := ok.(*OrderedMap)
	for k, want := range map[string]Node{"d": int64(1000), "h": int64(31), "o": int64(15), "b": int64(3), "neg": int64(-16)} {
		if v, _ := okm.Get(k); v != want {
			t.Errorf("%s = %v (%T), want %v", k, v, v, want)
		}
	}
	if v, _ := okm.Get("big"); v.(*big.Int).String() != "-18446744073709551617" {
		t.Errorf("big = %v", v)
	}

	// Empty containers and tuples.
	for _, input := range []string{`{}`, `[]`, `()`, `[(),{},[]]`} {
		if _, err := parsePython(input); err != nil {
			t.Errorf("parsePython(%q): %v", input, err)
		}
	}

	// pyString escapes control characters.
	out, err := encodePython(om("k", "a\"b\\c\nd\re\tf"+string(rune(0))+string(rune(0x7f)), "empty", arr(), "m", om()))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`\"`, `\\`, `\n`, `\r`, `\t`, `\x00`, `\x7f`, "[]", "{}"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if _, err := encodePython(om("m", om("n", math.NaN()))); !errors.Is(err, ErrUnsupportedStructure) {
		t.Error("nested NaN encoded")
	}
	if _, err := encodePython(arr(math.Inf(-1))); !errors.Is(err, ErrUnsupportedStructure) {
		t.Error("-Inf in array encoded")
	}
}

func TestJavaScriptEdges(t *testing.T) {
	n, err := parseJS(`({1: "num", 0x2: "hex", plus: +1, minus: -2, f: -1.5, s: "A", nested: {a: [{b: 1}]}})`)
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*OrderedMap)
	for k, want := range map[string]Node{"1": "num", "2": "hex", "plus": int64(1), "minus": int64(-2), "f": -1.5, "s": "A"} {
		if v, _ := m.Get(k); v != want {
			t.Errorf("%s = %v (%T), want %v", k, v, v, want)
		}
	}
	for _, bad := range []string{
		`({a: -"x"})`, `({a: +"x"})`, `({a: b})`, `({a: 1, ...rest})`,
		`({1.5: "float key"})`, `({[Symbol()]: 1})`, `({a: new Date()})`,
		`({a: 1} , {b: 2})`, `"x"`, `1`, `null`, `true`, `({a: i++})`,
	} {
		if _, err := parseJS(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	// Empty containers and nested elisions.
	if _, err := parseJS(`({a: {}, b: [], c: [,,]})`); err != nil {
		t.Fatal(err)
	}
	if _, err := parseJS(`([])`); err != nil {
		t.Fatal(err)
	}
}

func TestTOMLValueAndTableShapes(t *testing.T) {
	input := strings.Join([]string{
		`str = "s"`,
		`multi = """a\nb"""`,
		`literal = 'raw\no'`,
		`int = -7`,
		`float = 1.5e3`,
		`sci = 6.0`,
		`nan = nan`,
		`inf = inf`,
		`ninf = -inf`,
		`bool = false`,
		`arr = [1, 2, 3]`,
		`mixed = ["a", 1, true]`,
		`nested_arr = [[1, 2], [3]]`,
		`inline = {a = 1, b = "x"}`,
		`inline_aot = [{a = 1}, {a = 2}]`,
		`empty_arr = []`,
		`[t]`,
		`x = 1`,
		`[t.sub]`,
		`y = 2`,
		`[[items]]`,
		`id = 1`,
		`[items.meta]`,
		`tag = "first"`,
		`[[items]]`,
		`id = 2`,
		`[items.meta]`,
		`tag = "second"`,
		`[[items.notes]]`,
		`text = "n1"`,
		`[[items.notes]]`,
		`text = "n2"`,
	}, "\n") + "\n"

	n, err := parseTOML(input)
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*OrderedMap)
	if v, _ := m.Get("nan"); !math.IsNaN(v.(float64)) {
		t.Errorf("nan = %v", v)
	}
	if v, _ := m.Get("inf"); v != math.Inf(1) {
		t.Errorf("inf = %v", v)
	}
	if v, _ := m.Get("ninf"); v != math.Inf(-1) {
		t.Errorf("ninf = %v", v)
	}
	inlineAoT, _ := m.Get("inline_aot")
	elems, ok := inlineAoT.([]Node)
	if !ok || len(elems) != 2 {
		t.Fatalf("inline_aot = %#v", inlineAoT)
	}
	if v, _ := elems[1].(*OrderedMap).Get("a"); v != int64(2) {
		t.Errorf("inline_aot[1].a = %v", v)
	}

	// Nested arrays of tables keep per-element identity and order.
	items, _ := m.Get("items")
	itemArr := items.([]Node)
	if len(itemArr) != 2 {
		t.Fatalf("items = %#v", itemArr)
	}
	second := itemArr[1].(*OrderedMap)
	meta, _ := second.Get("meta")
	if tag, _ := meta.(*OrderedMap).Get("tag"); tag != "second" {
		t.Errorf("items[1].meta.tag = %v", tag)
	}
	notes, _ := second.Get("notes")
	noteArr, ok := notes.([]Node)
	if !ok || len(noteArr) != 2 {
		t.Fatalf("items[1].notes = %#v", notes)
	}
	if text, _ := noteArr[0].(*OrderedMap).Get("text"); text != "n1" {
		t.Errorf("notes[0].text = %v", text)
	}
	first := itemArr[0].(*OrderedMap)
	if _, hasNotes := first.Get("notes"); hasNotes {
		t.Error("notes leaked into items[0]")
	}

	// Re-encoding and re-parsing preserves the whole shape.
	out, err := encodeTOML(n)
	if err != nil {
		t.Fatal(err)
	}
	back, err := parseTOML(out)
	if err != nil || !nodeEqual(n, back, false) {
		t.Fatalf("round trip: %v\n%s", err, out)
	}

	// Quoted keys survive both directions.
	quoted, err := encodeTOML(om("a b", int64(1), "dotted.key", om("x", int64(2)), "", int64(3)))
	if err != nil {
		t.Fatal(err)
	}
	if qback, err := parseTOML(quoted); err != nil || qback.(*OrderedMap).Len() != 3 {
		t.Fatalf("quoted keys: %v\n%s", err, quoted)
	}
}

func TestTOMLErrorSurface(t *testing.T) {
	for _, input := range []string{
		"x = ", "[unclosed", "a = 1\na = 2\n", "= 1", "[[a]]\n[a]\nx=1\n",
		"a = 1\n[a]\nb = 2\n", "a = 0x", "a = [1,",
	} {
		if _, err := parseTOML(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	var pe *ParseError
	if _, err := parseTOML("x = ["); !errors.As(err, &pe) || pe.Format != TOML {
		t.Errorf("expected a TOML *ParseError, got %v (%T)", err, err)
	}
	// nan/inf reach TOML but not the other scalar-restricted targets.
	if _, err := encodeTOML(om("arr", arr(nil))); !errors.Is(err, ErrUnsupportedStructure) {
		t.Error("nil inside an array encoded")
	}
	if _, err := encodeTOML(om("t", om("u", uint64(math.MaxUint64)))); !errors.Is(err, ErrUnsupportedStructure) {
		t.Error("uint64 overflow encoded")
	}
	if _, err := encodeTOML(om("t", om("n", nil))); !errors.Is(err, ErrUnsupportedStructure) {
		t.Error("nested nil encoded")
	}
}

func TestTOMLTimeCarriers(t *testing.T) {
	n, err := parseTOML(strings.Join([]string{
		`offset = 1979-05-27T07:32:00-08:00`,
		`utc = 1979-05-27T07:32:00Z`,
		`frac = 1979-05-27T00:32:00.999999Z`,
		`local_dt = 1979-05-27T07:32:00`,
		`local_d = 1979-05-27`,
		`local_t = 07:32:00.5`,
		`arr = [1979-05-27, 1979-05-28]`,
	}, "\n") + "\n")
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*OrderedMap)
	for k, want := range map[string]string{
		"offset":   "1979-05-27T07:32:00-08:00",
		"utc":      "1979-05-27T07:32:00Z",
		"local_dt": "1979-05-27T07:32:00",
		"local_d":  "1979-05-27",
		"local_t":  "07:32:00.5",
	} {
		if v, _ := m.Get(k); v != want {
			t.Errorf("%s = %v, want %s", k, v, want)
		}
	}
	timeArr, _ := m.Get("arr")
	if items := timeArr.([]Node); len(items) != 2 || items[0] != "1979-05-27" {
		t.Errorf("arr = %#v", timeArr)
	}
}

func TestXMLValueCarriersAndDepth(t *testing.T) {
	// Encoding non-string scalars stringifies them; nil becomes an empty element.
	out, err := encodeXML(om("root", om(
		"@n", int64(1), "@f", 2.5, "@b", true, "@nil", nil,
		"i", int64(3), "u", uint64(4), "big", big.NewInt(5),
		"f", 6.5, "bool", false, "empty", nil,
		"list", arr("a", "b"),
	)))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`n="1"`, `f="2.5"`, `b="true"`, `nil=""`, "<i>3</i>", "<u>4</u>", "<big>5</big>", "<f>6.5</f>", "<bool>false</bool>", "<empty></empty>"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if _, err := encodeXML(om("root", om("bad", math.NaN()))); !errors.Is(err, ErrUnsupportedStructure) {
		t.Error("NaN encoded to XML")
	}
	if _, err := encodeXML(om("root", om("@bad", math.Inf(1)))); !errors.Is(err, ErrUnsupportedStructure) {
		t.Error("Inf attribute encoded")
	}
	if _, err := encodeXML(om()); !errors.Is(err, ErrUnsupportedStructure) {
		t.Error("root-less map encoded")
	}

	// Comments, processing instructions and doctypes are ignored; split text
	// segments concatenate.
	n, err := parseXML(`<?xml version="1.0"?><!DOCTYPE r><r><!-- c -->a<?pi x?>b</r>`)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := n.(*OrderedMap).Get("r")
	if r != "ab" {
		t.Errorf("split text = %#v, want %q", r, "ab")
	}
	// An empty element parses to the empty string: design section 5 defines an
	// empty element as an absent #text, and the encoder emits <r></r> for "",
	// so this is what makes "" round-trip.
	for _, input := range []string{`<r/>`, `<r></r>`, "<r> </r>"} {
		n, err := parseXML(input)
		if err != nil {
			t.Fatal(err)
		}
		v, _ := n.(*OrderedMap).Get("r")
		if v != "" {
			t.Errorf("parseXML(%q) element = %#v, want %q", input, v, "")
		}
	}
	// An element with an attribute keeps its map form.
	if n, err := parseXML(`<r a="1"/>`); err != nil {
		t.Fatal(err)
	} else {
		v, _ := n.(*OrderedMap).Get("r")
		if m, ok := v.(*OrderedMap); !ok || m.Len() != 1 {
			t.Errorf("element with attribute = %#v", v)
		}
	}
	// The depth guard fires on deeply nested documents.
	if _, err := parseXML(deepXML(100)); err != nil {
		t.Fatalf("100 levels: %v", err)
	}
	if _, err := parseXML(deepXML(10001)); !errors.Is(err, ErrTooDeep) {
		t.Errorf("10001 levels err = %v, want ErrTooDeep", err)
	}
	for _, input := range []string{`<a></b>`, `<a>&bad;</a>`, `</a>`, `<a attr></a>`} {
		if _, err := parseXML(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

func TestYAMLRemainingBranches(t *testing.T) {
	n, err := parseYAML(strings.Join([]string{
		`empty_map: {}`,
		`empty_seq: []`,
		`null_tilde: ~`,
		`null_bare:`,
		`quoted: "1"`,
		`neg: -5`,
		`oct: 0o17`,
		`hex: 0x1f`,
		`exp: 1.2e3`,
		`upper_bool: TRUE`,
		`false_upper: False`,
		`plus_inf: +.inf`,
		`nested: {a: {b: [1, 2]}}`,
	}, "\n") + "\n")
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*OrderedMap)
	for k, want := range map[string]Node{
		"quoted": "1", "neg": int64(-5), "oct": int64(15), "hex": int64(31),
		"exp": 1200.0, "upper_bool": true, "false_upper": false, "plus_inf": math.Inf(1),
	} {
		if v, _ := m.Get(k); v != want {
			t.Errorf("%s = %v (%T), want %v", k, v, v, want)
		}
	}
	for _, k := range []string{"null_tilde", "null_bare"} {
		if v, _ := m.Get(k); v != nil {
			t.Errorf("%s = %v, want nil", k, v)
		}
	}
	if v, _ := m.Get("empty_map"); v.(*OrderedMap).Len() != 0 {
		t.Error("empty_map not empty")
	}
	if v, _ := m.Get("empty_seq"); len(v.([]Node)) != 0 {
		t.Error("empty_seq not empty")
	}

	for _, input := range []string{
		"a: 1\n b: 2\n", "[1, 2", "{a: 1", "a: !!int notanint\n",
		"a: !!float notafloat\n", "? [1]\n: 2\n", "1: x\n", "a: !!set {x}\n",
	} {
		if _, err := parseYAML(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	// Non-container roots are rejected.
	for _, input := range []string{"5\n", "just a string\n", "true\n", "null\n"} {
		if _, err := parseYAML(input); !errors.Is(err, ErrTopLevelScalar) {
			t.Errorf("parseYAML(%q) err = %v, want ErrTopLevelScalar", input, err)
		}
	}
	// Encoding covers every carrier, including nested empties.
	out, err := encodeYAML(om("u", uint64(math.MaxUint64), "big", new(big.Int).Lsh(big.NewInt(1), 80),
		"empty_map", om(), "empty_arr", arr(), "nil", nil, "arr", arr(int64(1), om("k", "v"))))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"18446744073709551615", "1208925819614629174706176", "{}", "[]", "null"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
}

func TestOrderedMapRemainingBranches(t *testing.T) {
	m := NewOrderedMap()
	if _, ok := m.Get("missing"); ok {
		t.Error("Get on empty map reported a hit")
	}
	if m.Delete("missing") {
		t.Error("Delete on empty map reported success")
	}
	if len(m.Keys()) != 0 || m.Len() != 0 {
		t.Error("empty map is not empty")
	}
	// Deleting the only entry empties both the index and the list.
	m.Set("only", int64(1))
	if !m.Delete("only") || m.Len() != 0 || len(m.Keys()) != 0 {
		t.Error("deleting the sole entry left state behind")
	}
	// Re-adding after emptying rebuilds the list head/tail.
	m.Set("a", int64(1))
	m.Set("b", int64(2))
	if got := strings.Join(m.Keys(), ","); got != "a,b" {
		t.Errorf("keys after rebuild = %s", got)
	}
	// Clone of an empty map is independent.
	empty := NewOrderedMap()
	clone := empty.Clone()
	clone.Set("x", int64(1))
	if empty.Len() != 0 {
		t.Error("clone wrote through to the source")
	}
	// All() over an empty map yields nothing.
	for range empty.All() {
		t.Error("All() yielded an entry for an empty map")
	}
}

func TestParseAndEncodeDepthGuards(t *testing.T) {
	// Moderate nesting is accepted by every format that can express it.
	for _, tc := range []struct {
		f     Format
		input string
	}{
		{JSON, deepJSON(100)},
		{YAML, deepYAML(100)},
		{Lua, deepLua(100)},
		{Python, deepPython(100)},
		{JS, deepJS(100)},
		{XML, deepXML(100)},
	} {
		if _, err := Parse(tc.input, tc.f); err != nil {
			t.Errorf("Parse(%s, 100 levels): %v", tc.f, err)
		}
	}
	// Encoding a tree past the guard fails before touching any format writer.
	for _, f := range allFormats {
		if _, err := Encode(deepMaps(10001), f); !errors.Is(err, ErrTooDeep) {
			t.Errorf("Encode(%s, 10001 levels) err = %v, want ErrTooDeep", f, err)
		}
	}
}
