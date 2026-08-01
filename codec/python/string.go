package python

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/aura-studio/structure/v2/node"
)

// isStringPrefix reports whether c can start a string prefix (r/u/b/f).
func isStringPrefix(c byte) bool {
	switch c {
	case 'r', 'R', 'u', 'U', 'b', 'B', 'f', 'F':
		return true
	}
	return false
}

// looksLikePrefixedString reports whether the run of prefix letters at the
// cursor is actually followed by a quote. Without this check `False` would be
// read as an `F`-prefixed string instead of the boolean literal.
func (p *parser) looksLikePrefixedString() bool {
	i := p.pos
	for i < len(p.src) && isStringPrefix(p.src[i]) {
		i++
	}
	return i < len(p.src) && (p.src[i] == '\'' || p.src[i] == '"')
}

// parsePrefixedString handles r/u/b/f prefixed strings; b and f are errors.
func (p *parser) parsePrefixedString() (node.Node, error) {
	var flags [256]bool
	for p.pos < len(p.src) && isStringPrefix(p.src[p.pos]) {
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
func (p *parser) parseString() (node.Node, error) {
	return p.parseStringBody(false)
}

// parseStringBody reads a quoted string at the current quote character. The
// delimiter is held in q, not quote, so it does not shadow the encoder's quote
// function.
func (p *parser) parseStringBody(raw bool) (node.Node, error) {
	q := p.peek()
	triple := p.pos+3 <= len(p.src) && p.src[p.pos:p.pos+3] == strings.Repeat(string(q), 3)
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
		if c == q {
			if triple {
				if p.pos+3 <= len(p.src) && p.src[p.pos:p.pos+3] == strings.Repeat(string(q), 3) {
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
func (p *parser) parseEscape() (rune, error) {
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
		// \0 (or \ooo octal below); handle plain \0 here, octal via the reparse.
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

func (p *parser) parseOctalEscape() (rune, error) {
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

func (p *parser) parseFixedHexEscape(n int) (rune, error) {
	if p.pos+n > len(p.src) {
		return 0, p.errorf("truncated hex escape")
	}
	hexText := p.src[p.pos : p.pos+n]
	for i := 0; i < n; i++ {
		if !isHexDigit(p.src[p.pos+i]) {
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
