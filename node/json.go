package node

import (
	"fmt"
	"strconv"
	"strings"
)

// Compact JSON rendering lives in this package because OrderedMap implements
// json.Marshaler: a method must be declared in its receiver type's package, and
// codec/json imports node, so putting this renderer there would close an import
// cycle. codec/json's pretty-printer builds on these two functions rather than
// duplicating them.
//
// encoding/json cannot stand in for MarshalNode: it HTML-escapes < > and &, and
// renders float64(1) as "1" where FormatFloat emits "1.0" to keep the float type
// distinguishable on the way back in.

// lineSep and paraSep are U+2028 LINE SEPARATOR and U+2029 PARAGRAPH SEPARATOR.
// They are legal unescaped in JSON but are line terminators in pre-ES2019
// JavaScript, so JSONEscape always escapes them: the JS encoder emits JSON text
// verbatim and would otherwise produce a source file older engines reject.
// Written as code points rather than literals so the escaping cannot be undone
// by an editor silently normalizing the character.
const (
	lineSep = 0x2028
	paraSep = 0x2029
)

// JSONEscape renders s as a JSON string literal (double-quoted, minimal
// escapes).
func JSONEscape(s string) string {
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
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 || r == lineSep || r == paraSep {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// MarshalNode renders any Node as compact JSON bytes. It backs
// OrderedMap.MarshalJSON; codec/json uses its own indenting writer.
func MarshalNode(n Node) ([]byte, error) {
	switch v := n.(type) {
	case nil:
		return []byte("null"), nil
	case bool:
		return []byte(strconv.FormatBool(v)), nil
	case string:
		return []byte(JSONEscape(v)), nil
	case *OrderedMap:
		if v == nil {
			return nil, invalidf("invalid node: nil *OrderedMap")
		}
		return v.MarshalJSON()
	case []Node:
		var b strings.Builder
		b.WriteByte('[')
		for i, e := range v {
			if i > 0 {
				b.WriteByte(',')
			}
			part, err := MarshalNode(e)
			if err != nil {
				return nil, err
			}
			b.Write(part)
		}
		b.WriteByte(']')
		return []byte(b.String()), nil
	default:
		if IsNumeric(n) {
			s, err := FormatNumber(n)
			if err != nil {
				return nil, err
			}
			return []byte(s), nil
		}
		return nil, invalidf("invalid node type %T", n)
	}
}
