// Package json parses and encodes JSON text as a node.Node tree, preserving
// object key order and full integer precision.
package json

import (
	stdjson "encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"

	"github.com/aura-studio/structure/v2/format"
	"github.com/aura-studio/structure/v2/node"
)

// Parse decodes a JSON document into a node.Node tree, preserving object key
// order (via Token()+UseNumber) and full integer precision (int64 -> uint64 ->
// *big.Int ladder from the raw number text).
func Parse(input string) (node.Node, error) {
	p := &parser{input: input, dec: stdjson.NewDecoder(strings.NewReader(input))}
	p.dec.UseNumber()

	tok, err := p.dec.Token()
	if err == io.EOF {
		return nil, format.New(format.JSON, 0, 0, "empty input")
	}
	if err != nil {
		return nil, p.wrapErr(err)
	}

	var root node.Node
	switch d := tok.(type) {
	case stdjson.Delim:
		switch d {
		case '{':
			root, err = p.object(1)
		case '[':
			root, err = p.array(1)
		default:
			return nil, p.errorf("unexpected delimiter %q", string(d))
		}
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("%w: JSON root is %s", node.ErrTopLevelScalar, tokenName(tok))
	}

	// Reject trailing data after the root value.
	if p.dec.More() {
		return nil, p.errorf("trailing data after JSON document")
	}
	if _, err := p.dec.Token(); err != io.EOF {
		if err != nil {
			return nil, p.wrapErr(err)
		}
		return nil, p.errorf("trailing data after JSON document")
	}
	return root, nil
}

// parser pairs the decoder with the source text, so that the byte offsets
// encoding/json reports can be turned into line/column positions.
type parser struct {
	input string
	dec   *stdjson.Decoder
}

// pos returns the 1-based line/column just past the most recent token.
func (p *parser) pos() (int, int) {
	if p.dec == nil {
		return 0, 0
	}
	return format.LineCol(p.input, p.dec.InputOffset())
}

func (p *parser) errorf(msg string, args ...any) error {
	line, col := p.pos()
	return format.New(format.JSON, line, col, msg, args...)
}

// wrapErr converts an encoding/json error into a positioned *format.ParseError.
func (p *parser) wrapErr(err error) error {
	if errors.Is(wrapErr(err), node.ErrTooDeep) {
		return node.ErrTooDeep
	}
	// A SyntaxError carries the exact offset; otherwise fall back to the
	// decoder's current position.
	var se *stdjson.SyntaxError
	line, col := p.pos()
	if errors.As(err, &se) {
		line, col = format.LineCol(p.input, se.Offset)
	}
	return format.Wrap(format.JSON, line, col, err, err.Error())
}

// object parses an object body; the opening '{' was already consumed.
func (p *parser) object(depth int) (node.Node, error) {
	if err := node.TooDeep(depth); err != nil {
		return nil, err
	}
	m := node.NewOrderedMap()
	for p.dec.More() {
		keyTok, err := p.dec.Token()
		if err != nil {
			return nil, p.wrapErr(err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, p.errorf("object key must be a string, got %s", tokenName(keyTok))
		}
		if _, dup := m.Get(key); dup {
			// InputOffset() points just past the key's closing quote; step back
			// over the quoted token so the error names the offending key itself.
			start := p.dec.InputOffset() - int64(len(key)) - 2
			line, col := format.LineCol(p.input, start)
			return nil, format.DuplicateKey(format.JSON, line, col, key)
		}
		valTok, err := p.dec.Token()
		if err != nil {
			return nil, p.wrapErr(err)
		}
		val, err := p.value(valTok, depth)
		if err != nil {
			return nil, err
		}
		m.Set(key, val)
	}
	// Consume the closing '}'.
	if _, err := p.dec.Token(); err != nil {
		return nil, p.wrapErr(err)
	}
	return m, nil
}

// array parses an array body; the opening '[' was already consumed.
func (p *parser) array(depth int) (node.Node, error) {
	if err := node.TooDeep(depth); err != nil {
		return nil, err
	}
	arr := []node.Node{}
	for p.dec.More() {
		tok, err := p.dec.Token()
		if err != nil {
			return nil, p.wrapErr(err)
		}
		val, err := p.value(tok, depth)
		if err != nil {
			return nil, err
		}
		arr = append(arr, val)
	}
	// Consume the closing ']'.
	if _, err := p.dec.Token(); err != nil {
		return nil, p.wrapErr(err)
	}
	return arr, nil
}

// value converts one Token into a Node, recursing into containers.
func (p *parser) value(tok stdjson.Token, depth int) (node.Node, error) {
	switch t := tok.(type) {
	case stdjson.Delim:
		switch t {
		case '{':
			return p.object(depth + 1)
		case '[':
			return p.array(depth + 1)
		}
		return nil, p.errorf("unexpected delimiter %q", string(t))
	case stdjson.Number:
		return numberNode(t)
	case string:
		return t, nil
	case bool:
		return t, nil
	case nil:
		return nil, nil // JSON null
	}
	return nil, p.errorf("unexpected token %s", tokenName(tok))
}

// numberNode maps a raw JSON number onto the carrier ladder:
// int64 -> uint64 -> *big.Int -> float64. 2^53+1 style integers stay exact.
func numberNode(n stdjson.Number) (node.Node, error) {
	s := n.String()
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i, nil
	}
	if u, err := strconv.ParseUint(s, 10, 64); err == nil {
		return u, nil
	}
	if b, ok := new(big.Int).SetString(s, 10); ok {
		return b, nil
	}
	f, err := n.Float64()
	if err != nil {
		return nil, wrapErr(err)
	}
	return f, nil
}

func tokenName(tok stdjson.Token) string {
	switch t := tok.(type) {
	case stdjson.Delim:
		return fmt.Sprintf("delimiter %q", string(t))
	case stdjson.Number:
		return fmt.Sprintf("number %s", string(t))
	case string:
		return "string"
	case bool:
		return "boolean"
	case nil:
		return "null"
	}
	return fmt.Sprintf("%T", tok)
}

// wrapErr converts an encoding/json error into a *format.ParseError, preserving
// sentinel matching where possible.
func wrapErr(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "exceeded max depth") {
		return node.ErrTooDeep
	}
	return format.Wrap(format.JSON, 0, 0, err, msg)
}
