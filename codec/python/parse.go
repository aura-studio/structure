// Package python parses and encodes the Python literal subset accepted by
// ast.literal_eval as a node.Node tree.
//
// The parser is hand-written recursive descent: no CPython, no cgo. Dicts,
// lists, tuples, strings, numbers, True/False/None are accepted; sets, bytes,
// f-strings, complex numbers and any name reference are rejected.
package python

import (
	"fmt"

	"github.com/aura-studio/structure/v2/format"
	"github.com/aura-studio/structure/v2/node"
)

// parser is a hand-written recursive descent parser over the source text.
type parser struct {
	src string
	pos int
}

// Parse decodes a Python literal into a node.Node tree.
func Parse(input string) (node.Node, error) {
	p := &parser{src: input}
	n, err := p.parseValue(1)
	if err != nil {
		return nil, err
	}
	p.skipWS()
	if !p.eof() {
		return nil, p.errorf("trailing data after literal")
	}
	switch n.(type) {
	case *node.OrderedMap, []node.Node:
		return n, nil
	default:
		return nil, fmt.Errorf("%w: Python root is %T", node.ErrTopLevelScalar, n)
	}
}

func (p *parser) eof() bool { return p.pos >= len(p.src) }

func (p *parser) peek() byte {
	if p.eof() {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) lineCol() (int, int) {
	return format.LineCol(p.src, int64(p.pos))
}

// errorf builds a positioned *format.ParseError. The message parameter is named
// msg, not format, so it does not shadow the format package.
func (p *parser) errorf(msg string, args ...any) error {
	line, col := p.lineCol()
	return format.New(format.Python, line, col, msg, args...)
}

// skipWS skips whitespace (including newlines) and # comments.
func (p *parser) skipWS() {
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

func (p *parser) parseValue(depth int) (node.Node, error) {
	if err := node.TooDeep(depth); err != nil {
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
	case isStringPrefix(c) && p.looksLikePrefixedString():
		return p.parsePrefixedString()
	case isDigit(c) || c == '.':
		return p.parseNumber(false)
	case c == '-' || c == '+':
		neg := c == '-'
		p.pos++
		p.skipWS()
		if p.eof() || (!isDigit(p.peek()) && p.peek() != '.') {
			return nil, p.errorf("unary %q must apply to a number literal", string(c))
		}
		return p.parseNumber(neg)
	case isNameStart(c):
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

func isDigit(c byte) bool     { return c >= '0' && c <= '9' }
func isHexDigit(c byte) bool  { return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }
func isNameStart(c byte) bool { return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isNameChar(c byte) bool  { return isNameStart(c) || isDigit(c) }

func (p *parser) scanName() string {
	start := p.pos
	for p.pos < len(p.src) && isNameChar(p.src[p.pos]) {
		p.pos++
	}
	return p.src[start:p.pos]
}

// parseDict parses {...}; empty {} is a dict, any comma/closing after a value
// without a colon means a set literal, which is rejected.
func (p *parser) parseDict(depth int) (node.Node, error) {
	p.pos++ // '{'
	p.skipWS()
	if p.peek() == '}' {
		p.pos++
		return node.NewOrderedMap(), nil
	}
	m := node.NewOrderedMap()
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
			return nil, format.DuplicateKey(format.Python, keyLine, keyCol, ks)
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
func (p *parser) parseList(depth int) (node.Node, error) {
	p.pos++ // '['
	p.skipWS()
	if p.peek() == ']' {
		p.pos++
		return []node.Node{}, nil
	}
	arr := []node.Node{}
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
func (p *parser) parseTuple(depth int) (node.Node, error) {
	p.pos++ // '('
	p.skipWS()
	if p.peek() == ')' {
		p.pos++
		return []node.Node{}, nil
	}
	arr := []node.Node{}
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
