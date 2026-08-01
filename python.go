package structure

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"unicode"
)

// pyParser is a hand-written recursive descent parser for the Python literal
// subset accepted by ast.literal_eval (Requirement 3.3). No CPython, no cgo.
type pyParser struct {
	src string
	pos int
}

func parsePython(input string) (Node, error) {
	p := &pyParser{src: input}
	n, err := p.parseValue(1)
	if err != nil {
		return nil, err
	}
	p.skipWS()
	if !p.eof() {
		return nil, p.errorf("trailing data after literal")
	}
	switch n.(type) {
	case *OrderedMap, []Node:
		return n, nil
	default:
		return nil, fmt.Errorf("%w: Python root is %T", ErrTopLevelScalar, n)
	}
}

func (p *pyParser) eof() bool { return p.pos >= len(p.src) }

func (p *pyParser) peek() byte {
	if p.eof() {
		return 0
	}
	return p.src[p.pos]
}

func (p *pyParser) lineCol() (int, int) {
	return xmlLineCol(p.src, int64(p.pos))
}

func (p *pyParser) errorf(format string, args ...any) error {
	line, col := p.lineCol()
	return newParseError(Python, line, col, format, args...)
}

// skipWS skips whitespace (including newlines) and # comments.
func (p *pyParser) skipWS() {
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch c {
		case ' ', '\t', '\n', '\r', '\f', '\v':
			p.pos++
		case '#':
			for p.pos < len(p.src) && p.src[p.pos] != '\n' {
				p.pos++
			}
		default:
			return
		}
	}
}

func (p *pyParser) parseValue(depth int) (Node, error) {
	if err := tooDeep(depth); err != nil {
		return nil, err
	}
	p.skipWS()
	if p.eof() {
		return nil, p.errorf("unexpected end of input")
	}
	c := p.peek()
	switch {
	case c == '{':
		return p.parseDict(depth)
	case c == '[':
		return p.parseList(depth)
	case c == '(':
		return p.parseTuple(depth)
	case c == '\'' || c == '"':
		s, err := p.parseString()
		if err != nil {
			return nil, err
		}
		// Reject implicit string concatenation ("a" "b").
		p.skipWS()
		if !p.eof() && (p.peek() == '\'' || p.peek() == '"') {
			return nil, p.errorf("implicit string concatenation is not supported")
		}
		return s, nil
	case isPyStringPrefix(c) && p.looksLikePrefixedString():
		return p.parsePrefixedString()
	case isPyDigit(c) || c == '.':
		return p.parseNumber(false)
	case c == '-' || c == '+':
		neg := c == '-'
		p.pos++
		p.skipWS()
		if p.eof() || (!isPyDigit(p.peek()) && p.peek() != '.') {
			return nil, p.errorf("unary %q must apply to a number literal", string(c))
		}
		return p.parseNumber(neg)
	case isPyNameStart(c):
		name := p.scanName()
		switch name {
		case "True":
			return true, nil
		case "False":
			return false, nil
		case "None":
			return nil, nil
		}
		return nil, p.errorf("name %q is not supported (only literals are allowed)", name)
	}
	return nil, p.errorf("unexpected character %q", string(c))
}

func isPyDigit(c byte) bool     { return c >= '0' && c <= '9' }
func isPyHexDigit(c byte) bool  { return isPyDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }
func isPyNameStart(c byte) bool { return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isPyNameChar(c byte) bool  { return isPyNameStart(c) || isPyDigit(c) }

// isPyStringPrefix reports whether c can start a string prefix (r/u/b/f).
func isPyStringPrefix(c byte) bool {
	switch c {
	case 'r', 'R', 'u', 'U', 'b', 'B', 'f', 'F':
		return true
	}
	return false
}

func (p *pyParser) scanName() string {
	start := p.pos
	for p.pos < len(p.src) && isPyNameChar(p.src[p.pos]) {
		p.pos++
	}
	return p.src[start:p.pos]
}

// parseDict parses {...}; empty {} is a dict, any comma/closing after a value
// without a colon means a set literal, which is rejected.
func (p *pyParser) parseDict(depth int) (Node, error) {
	p.pos++ // '{'
	p.skipWS()
	if p.peek() == '}' {
		p.pos++
		return NewOrderedMap(), nil
	}
	m := NewOrderedMap()
	for {
		p.skipWS()
		keyLine, keyCol := p.lineCol()
		key, err := p.parseValue(depth + 1)
		if err != nil {
			return nil, err
		}
		p.skipWS()
		if p.peek() != ':' {
			return nil, p.errorf("sets are not supported (dict entries need ':' after the key)")
		}
		ks, ok := key.(string)
		if !ok {
			return nil, p.errorf("only string dict keys are supported (got %T)", key)
		}
		if _, dup := m.Get(ks); dup {
			return nil, duplicateKeyError(Python, keyLine, keyCol, ks)
		}
		p.pos++ // ':'
		val, err := p.parseValue(depth + 1)
		if err != nil {
			return nil, err
		}
		m.Set(ks, val)
		p.skipWS()
		if p.peek() == ',' {
			p.pos++
			p.skipWS()
			if p.peek() == '}' {
				p.pos++
				return m, nil
			}
			continue
		}
		if p.peek() == '}' {
			p.pos++
			return m, nil
		}
		return nil, p.errorf("expected ',' or '}' in dict, got %q", string(p.peek()))
	}
}

// parseList parses [...] into an array.
func (p *pyParser) parseList(depth int) (Node, error) {
	p.pos++ // '['
	p.skipWS()
	if p.peek() == ']' {
		p.pos++
		return []Node{}, nil
	}
	arr := []Node{}
	for {
		v, err := p.parseValue(depth + 1)
		if err != nil {
			return nil, err
		}
		arr = append(arr, v)
		p.skipWS()
		if p.peek() == ',' {
			p.pos++
			p.skipWS()
			if p.peek() == ']' {
				p.pos++
				return arr, nil
			}
			continue
		}
		if p.peek() == ']' {
			p.pos++
			return arr, nil
		}
		return nil, p.errorf("expected ',' or ']' in list, got %q", string(p.peek()))
	}
}

// parseTuple parses (...) and normalizes it into an array.
func (p *pyParser) parseTuple(depth int) (Node, error) {
	p.pos++ // '('
	p.skipWS()
	if p.peek() == ')' {
		p.pos++
		return []Node{}, nil
	}
	arr := []Node{}
	for {
		v, err := p.parseValue(depth + 1)
		if err != nil {
			return nil, err
		}
		arr = append(arr, v)
		p.skipWS()
		if p.peek() == ',' {
			p.pos++
			p.skipWS()
			if p.peek() == ')' {
				p.pos++
				return arr, nil
			}
			continue
		}
		if p.peek() == ')' {
			p.pos++
			// A single value without a trailing comma is a parenthesized
			// expression, not a tuple: ast.literal_eval("(1)") is the scalar 1.
			// Only a comma builds a one-element tuple, e.g. "(1,)".
			if len(arr) == 1 {
				return arr[0], nil
			}
			return arr, nil
		}
		return nil, p.errorf("expected ',' or ')' in tuple, got %q", string(p.peek()))
	}
}

// looksLikePrefixedString reports whether the run of prefix letters at the
// cursor is actually followed by a quote. Without this check `False` would be
// read as an `F`-prefixed string instead of the boolean literal.
func (p *pyParser) looksLikePrefixedString() bool {
	i := p.pos
	for i < len(p.src) && isPyStringPrefix(p.src[i]) {
		i++
	}
	return i < len(p.src) && (p.src[i] == '\'' || p.src[i] == '"')
}

// parsePrefixedString handles r/u/b/f prefixed strings; b and f are errors.
func (p *pyParser) parsePrefixedString() (Node, error) {
	var flags [256]bool
	for p.pos < len(p.src) && isPyStringPrefix(p.src[p.pos]) {
		flags[lowerASCII(p.src[p.pos])] = true
		p.pos++
		if p.pos > 0 && flags['r'] && flags['u'] && flags['b'] {
			break
		}
	}
	if p.eof() || (p.peek() != '\'' && p.peek() != '"') {
		return nil, p.errorf("unsupported name or string prefix")
	}
	if flags['b'] {
		return nil, p.errorf("bytes strings are not supported")
	}
	if flags['f'] {
		return nil, p.errorf("f-strings are not supported")
	}
	return p.parseStringBody(flags['r'])
}

func lowerASCII(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// parseString parses a quote-starting string literal.
func (p *pyParser) parseString() (Node, error) {
	return p.parseStringBody(false)
}

// parseStringBody reads a quoted string at the current quote character.
func (p *pyParser) parseStringBody(raw bool) (Node, error) {
	quote := p.peek()
	triple := p.pos+3 <= len(p.src) && p.src[p.pos:p.pos+3] == strings.Repeat(string(quote), 3)
	if triple {
		p.pos += 3
	} else {
		p.pos++
	}
	var b strings.Builder
	for {
		if p.eof() {
			return nil, p.errorf("unterminated string literal")
		}
		c := p.src[p.pos]
		if c == '\\' {
			if p.pos+1 >= len(p.src) {
				return nil, p.errorf("unterminated string escape")
			}
			if raw {
				// Raw: keep backslash and the following char literally; a
				// backslash still prevents the quote from terminating.
				b.WriteByte('\\')
				b.WriteByte(p.src[p.pos+1])
				p.pos += 2
				continue
			}
			p.pos++ // consume backslash
			r, err := p.parseEscape()
			if err != nil {
				return nil, err
			}
			if r >= 0 { // negative rune = line continuation, dropped
				b.WriteRune(r)
			}
			continue
		}
		if c == quote {
			if triple {
				if p.pos+3 <= len(p.src) && p.src[p.pos:p.pos+3] == strings.Repeat(string(quote), 3) {
					p.pos += 3
					return b.String(), nil
				}
				b.WriteByte(c)
				p.pos++
				continue
			}
			p.pos++
			return b.String(), nil
		}
		if !triple && (c == '\n') {
			return nil, p.errorf("unterminated string literal (newline in single-line string)")
		}
		b.WriteByte(c)
		p.pos++
	}
}

// parseEscape reads one escape sequence after the backslash.
func (p *pyParser) parseEscape() (rune, error) {
	c := p.src[p.pos]
	p.pos++
	switch c {
	case 'n':
		return '\n', nil
	case 't':
		return '\t', nil
	case 'r':
		return '\r', nil
	case '0':
		// \0 (or \ooo octal below); handle plain \0 here, octal via fallthrough rule
		if p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '7' {
			p.pos-- // reparse as octal
			return p.parseOctalEscape()
		}
		return 0, nil
	case '1', '2', '3', '4', '5', '6', '7':
		p.pos--
		return p.parseOctalEscape()
	case '\\':
		return '\\', nil
	case '\'':
		return '\'', nil
	case '"':
		return '"', nil
	case 'a':
		return '\a', nil
	case 'b':
		return '\b', nil
	case 'f':
		return '\f', nil
	case 'v':
		return '\v', nil
	case '\n':
		return -1, nil // line continuation: caller must drop it
	case 'x':
		return p.parseFixedHexEscape(2)
	case 'u':
		return p.parseFixedHexEscape(4)
	case 'U':
		return p.parseFixedHexEscape(8)
	case 'N':
		return 0, p.errorf("\\N{name} escapes are not supported")
	}
	return 0, p.errorf("invalid escape sequence \\%c", c)
}

func (p *pyParser) parseOctalEscape() (rune, error) {
	start := p.pos
	v := 0
	for i := 0; i < 3 && p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '7'; i++ {
		v = v*8 + int(p.src[p.pos]-'0')
		p.pos++
	}
	if p.pos == start {
		return 0, p.errorf("invalid octal escape")
	}
	if v > 0o377 {
		return 0, p.errorf("octal escape value %d out of range 0..0o377", v)
	}
	return rune(v), nil
}

func (p *pyParser) parseFixedHexEscape(n int) (rune, error) {
	if p.pos+n > len(p.src) {
		return 0, p.errorf("truncated hex escape")
	}
	hexText := p.src[p.pos : p.pos+n]
	for i := 0; i < n; i++ {
		if !isPyHexDigit(p.src[p.pos+i]) {
			return 0, p.errorf("invalid hex escape %q", hexText)
		}
	}
	v, err := strconv.ParseUint(hexText, 16, 32)
	if err != nil {
		return 0, p.errorf("invalid hex escape %q", hexText)
	}
	p.pos += n
	// CPython admits \u escapes up to U+FFFF and stores even lone surrogates
	// in a str, but a Go string holds UTF-8: writing a surrogate or a value
	// above unicode.MaxRune would silently yield U+FFFD. Reject rather than
	// corrupt (the package's reject-unrepresentable policy).
	name := byte('x')
	switch n {
	case 4:
		name = 'u'
	case 8:
		name = 'U'
	}
	if v > unicode.MaxRune {
		return 0, p.errorf("\\%c%s is out of the Unicode range (U+%X > U+10FFFF)", name, hexText, v)
	}
	if v >= 0xD800 && v <= 0xDFFF {
		return 0, p.errorf("\\%c%s is a lone surrogate (U+%X), which is not representable in UTF-8", name, hexText, v)
	}
	return rune(v), nil
}

// parseNumber parses an int or float literal (without sign).
func (p *pyParser) parseNumber(neg bool) (Node, error) {
	start := p.pos
	isFloat := false
	base := 10

	if p.peek() == '0' && p.pos+1 < len(p.src) {
		switch p.src[p.pos+1] {
		case 'x', 'X':
			base = 16
			p.pos += 2
			p.scanDigits(16)
		case 'o', 'O':
			base = 8
			p.pos += 2
			p.scanDigits(8)
		case 'b', 'B':
			base = 2
			p.pos += 2
			p.scanDigits(2)
		}
	}
	if base == 10 {
		p.scanDigits(10)
		if p.pos < len(p.src) && p.src[p.pos] == '.' {
			isFloat = true
			p.pos++
			p.scanDigits(10)
		}
		if p.pos < len(p.src) && (p.src[p.pos] == 'e' || p.src[p.pos] == 'E') {
			isFloat = true
			p.pos++
			if p.pos < len(p.src) && (p.src[p.pos] == '+' || p.src[p.pos] == '-') {
				p.pos++
			}
			p.scanDigits(10)
		}
	}
	// Complex suffix is rejected.
	if p.pos < len(p.src) && (p.src[p.pos] == 'j' || p.src[p.pos] == 'J') {
		return nil, p.errorf("complex numbers are not supported")
	}
	lit := p.src[start:p.pos]
	if err := pyValidateNumberLit(lit, base); err != nil {
		return nil, p.errorf("%s", err.Error())
	}
	clean := strings.ReplaceAll(lit, "_", "")
	if isFloat {
		f, err := strconv.ParseFloat(clean, 64)
		if err != nil {
			return nil, p.errorf("invalid float literal %q", lit)
		}
		if neg {
			f = -f
		}
		return f, nil
	}
	parseBase := base
	if base != 10 {
		// clean still includes 0x/0o/0b; base 0 consumes that prefix.
		parseBase = 0
	}
	if i, err := strconv.ParseInt(clean, parseBase, 64); err == nil {
		if neg {
			i = -i
		}
		return i, nil
	}
	b, ok := new(big.Int).SetString(clean, parseBase)
	if !ok {
		return nil, p.errorf("invalid integer literal %q", lit)
	}
	if neg {
		b.Neg(b)
	}
	return luaBigResult(b), nil
}

// scanDigits advances over digits of the given base plus underscores.
func (p *pyParser) scanDigits(base int) {
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		ok := false
		switch base {
		case 2:
			ok = c == '0' || c == '1'
		case 8:
			ok = c >= '0' && c <= '7'
		case 10:
			ok = isPyDigit(c)
		case 16:
			ok = isPyHexDigit(c)
		}
		if ok || c == '_' {
			p.pos++
			continue
		}
		return
	}
}

// pyValidateNumberLit enforces Python's underscore and leading-zero rules.
func pyValidateNumberLit(lit string, base int) error {
	if lit == "" {
		return fmt.Errorf("invalid number literal")
	}
	// Underscores sit between digits, or directly after a base prefix
	// (PEP 515 permits 0x_1f).
	for i, c := range lit {
		if c != '_' {
			continue
		}
		if i == 0 || i == len(lit)-1 {
			return fmt.Errorf("invalid underscore placement in %q", lit)
		}
		afterBasePrefix := base != 10 && i == 2
		if !afterBasePrefix && !isPyDigitLike(lit[i-1], base) {
			return fmt.Errorf("invalid underscore placement in %q", lit)
		}
		if !isPyDigitLike(lit[i+1], base) {
			return fmt.Errorf("invalid underscore placement in %q", lit)
		}
	}
	// No leading zeros in decimal integers.
	if base == 10 && !strings.ContainsAny(lit, ".eE") {
		core := strings.ReplaceAll(lit, "_", "")
		if len(core) > 1 && core[0] == '0' {
			return fmt.Errorf("leading zeros are not permitted in %q", lit)
		}
	}
	return nil
}

func isPyDigitLike(c byte, base int) bool {
	switch base {
	case 2:
		return c == '0' || c == '1'
	case 8:
		return c >= '0' && c <= '7'
	case 16:
		return isPyHexDigit(c)
	}
	return isPyDigit(c)
}

// encodePython renders a Node tree as a Python literal (repr contract:
// feeding the output back to literal_eval yields the same value).
func encodePython(n Node) (string, error) {
	if err := validate(n); err != nil {
		return "", err
	}
	var b strings.Builder
	if err := writePythonValue(&b, n, 0); err != nil {
		return "", err
	}
	b.WriteByte('\n')
	return b.String(), nil
}

func writePythonValue(b *strings.Builder, n Node, indent int) error {
	switch v := n.(type) {
	case nil:
		b.WriteString("None")
	case bool:
		if v {
			b.WriteString("True")
		} else {
			b.WriteString("False")
		}
	case string:
		b.WriteString(pyString(v))
	case *OrderedMap:
		return writePythonDict(b, v, indent)
	case []Node:
		return writePythonList(b, v, indent)
	default:
		if isNumeric(n) {
			s, err := formatNumber(n)
			if err != nil {
				return fmt.Errorf("python: %w", err)
			}
			b.WriteString(s)
			return nil
		}
		return fmt.Errorf("structure: invalid node type %T", n)
	}
	return nil
}

func writePythonDict(b *strings.Builder, m *OrderedMap, indent int) error {
	if m.Len() == 0 {
		b.WriteString("{}")
		return nil
	}
	b.WriteString("{\n")
	i := 0
	for k, v := range m.All() {
		writeIndent(b, indent+1)
		b.WriteString(pyString(k))
		b.WriteString(": ")
		if err := writePythonValue(b, v, indent+1); err != nil {
			return err
		}
		if i < m.Len()-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
		i++
	}
	writeIndent(b, indent)
	b.WriteByte('}')
	return nil
}

func writePythonList(b *strings.Builder, arr []Node, indent int) error {
	if len(arr) == 0 {
		b.WriteString("[]")
		return nil
	}
	b.WriteString("[\n")
	for i, e := range arr {
		writeIndent(b, indent+1)
		if err := writePythonValue(b, e, indent+1); err != nil {
			return err
		}
		if i < len(arr)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	writeIndent(b, indent)
	b.WriteByte(']')
	return nil
}

// pyString renders a double-quoted Python string literal.
func pyString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\x%02x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
