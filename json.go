package structure

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
)

// parseJSON decodes a JSON document into a Node tree, preserving object key
// order (via Token()+UseNumber) and full integer precision (int64 -> uint64
// -> *big.Int ladder from the raw number text).
func parseJSON(input string) (Node, error) {
	p := &jsonParser{input: input, dec: json.NewDecoder(strings.NewReader(input))}
	p.dec.UseNumber()

	tok, err := p.dec.Token()
	if err == io.EOF {
		return nil, newParseError(JSON, 0, 0, "empty input")
	}
	if err != nil {
		return nil, p.wrapErr(err)
	}

	var root Node
	switch d := tok.(type) {
	case json.Delim:
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
		return nil, fmt.Errorf("%w: JSON root is %s", ErrTopLevelScalar, jsonTokenName(tok))
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

// jsonParser pairs the decoder with the source text, so that the byte offsets
// encoding/json reports can be turned into line/column positions.
type jsonParser struct {
	input string
	dec   *json.Decoder
}

// pos returns the 1-based line/column just past the most recent token.
func (p *jsonParser) pos() (int, int) {
	if p.dec == nil {
		return 0, 0
	}
	return xmlLineCol(p.input, p.dec.InputOffset())
}

func (p *jsonParser) errorf(format string, args ...any) error {
	line, col := p.pos()
	return newParseError(JSON, line, col, format, args...)
}

// wrapErr converts an encoding/json error into a positioned *ParseError.
func (p *jsonParser) wrapErr(err error) error {
	if errors.Is(wrapJSONErr(err), ErrTooDeep) {
		return ErrTooDeep
	}
	// A SyntaxError carries the exact offset; otherwise fall back to the
	// decoder's current position.
	var se *json.SyntaxError
	line, col := p.pos()
	if errors.As(err, &se) {
		line, col = xmlLineCol(p.input, se.Offset)
	}
	return wrapParseError(JSON, line, col, err, err.Error())
}

// object parses an object body; the opening '{' was already consumed.
func (p *jsonParser) object(depth int) (Node, error) {
	if err := tooDeep(depth); err != nil {
		return nil, err
	}
	m := NewOrderedMap()
	for p.dec.More() {
		keyTok, err := p.dec.Token()
		if err != nil {
			return nil, p.wrapErr(err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, p.errorf("object key must be a string, got %s", jsonTokenName(keyTok))
		}
		if _, dup := m.Get(key); dup {
			// InputOffset() points just past the key's closing quote; step back
			// over the quoted token so the error names the offending key itself.
			start := p.dec.InputOffset() - int64(len(key)) - 2
			line, col := xmlLineCol(p.input, start)
			return nil, duplicateKeyError(JSON, line, col, key)
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
func (p *jsonParser) array(depth int) (Node, error) {
	if err := tooDeep(depth); err != nil {
		return nil, err
	}
	arr := []Node{}
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
func (p *jsonParser) value(tok json.Token, depth int) (Node, error) {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			return p.object(depth + 1)
		case '[':
			return p.array(depth + 1)
		}
		return nil, p.errorf("unexpected delimiter %q", string(t))
	case json.Number:
		return jsonNumberNode(t)
	case string:
		return t, nil
	case bool:
		return t, nil
	case nil:
		return nil, nil // JSON null
	}
	return nil, p.errorf("unexpected token %s", jsonTokenName(tok))
}

// jsonNumberNode maps a raw JSON number onto the carrier ladder:
// int64 -> uint64 -> *big.Int -> float64. 2^53+1 style integers stay exact.
func jsonNumberNode(n json.Number) (Node, error) {
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
		return nil, wrapJSONErr(err)
	}
	return f, nil
}

func jsonTokenName(tok json.Token) string {
	switch t := tok.(type) {
	case json.Delim:
		return fmt.Sprintf("delimiter %q", string(t))
	case json.Number:
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

// wrapJSONErr converts an encoding/json error into a *ParseError, preserving
// sentinel matching where possible.
func wrapJSONErr(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "exceeded max depth") {
		return ErrTooDeep
	}
	return &ParseError{Format: JSON, Msg: msg, err: err}
}

// encodeJSON renders a Node tree as indented (2-space) JSON text.
func encodeJSON(n Node) (string, error) {
	if err := validate(n); err != nil {
		return "", err
	}
	var b strings.Builder
	if err := writeJSONValue(&b, n, 0); err != nil {
		return "", err
	}
	b.WriteByte('\n')
	return b.String(), nil
}

// writeJSONValue emits n with the given indentation level.
func writeJSONValue(b *strings.Builder, n Node, indent int) error {
	switch v := n.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(v))
	case string:
		b.WriteString(jsonEscape(v))
	case *OrderedMap:
		return writeJSONObject(b, v, indent)
	case []Node:
		return writeJSONArray(b, v, indent)
	default:
		if isNumeric(n) {
			s, err := formatNumber(n)
			if err != nil {
				return fmt.Errorf("json: %w", err)
			}
			b.WriteString(s)
			return nil
		}
		return fmt.Errorf("structure: invalid node type %T", n)
	}
	return nil
}

func writeJSONObject(b *strings.Builder, m *OrderedMap, indent int) error {
	if m.Len() == 0 {
		b.WriteString("{}")
		return nil
	}
	b.WriteString("{\n")
	i := 0
	for k, val := range m.All() {
		writeIndent(b, indent+1)
		b.WriteString(jsonEscape(k))
		b.WriteString(": ")
		if err := writeJSONValue(b, val, indent+1); err != nil {
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

func writeJSONArray(b *strings.Builder, arr []Node, indent int) error {
	if len(arr) == 0 {
		b.WriteString("[]")
		return nil
	}
	b.WriteString("[\n")
	for i, e := range arr {
		writeIndent(b, indent+1)
		if err := writeJSONValue(b, e, indent+1); err != nil {
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

func writeIndent(b *strings.Builder, level int) {
	for i := 0; i < level; i++ {
		b.WriteString("  ")
	}
}

// jsonMarshalNode renders any Node as compact JSON bytes (used by
// OrderedMap.MarshalJSON).
func jsonMarshalNode(n Node) ([]byte, error) {
	switch v := n.(type) {
	case nil:
		return []byte("null"), nil
	case bool:
		return []byte(strconv.FormatBool(v)), nil
	case string:
		return []byte(jsonEscape(v)), nil
	case *OrderedMap:
		if v == nil {
			return nil, errors.New("structure: invalid node: nil *OrderedMap")
		}
		return v.MarshalJSON()
	case []Node:
		var b strings.Builder
		b.WriteByte('[')
		for i, e := range v {
			if i > 0 {
				b.WriteByte(',')
			}
			part, err := jsonMarshalNode(e)
			if err != nil {
				return nil, err
			}
			b.Write(part)
		}
		b.WriteByte(']')
		return []byte(b.String()), nil
	default:
		if isNumeric(n) {
			s, err := formatNumber(n)
			if err != nil {
				return nil, err
			}
			return []byte(s), nil
		}
		return nil, fmt.Errorf("structure: invalid node type %T", n)
	}
}
