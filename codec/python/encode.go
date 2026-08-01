package python

import (
	"fmt"
	"strings"

	"github.com/aura-studio/structure/v2/codec/internal/emit"
	"github.com/aura-studio/structure/v2/node"
)

// Encode renders a node.Node tree as a Python literal (repr contract: feeding
// the output back to literal_eval yields the same value).
func Encode(n node.Node) (string, error) {
	if err := node.Validate(n); err != nil {
		return "", err
	}
	var b strings.Builder
	if err := writeValue(&b, n, 0); err != nil {
		return "", err
	}
	b.WriteByte('\n')
	return b.String(), nil
}

func writeValue(b *strings.Builder, n node.Node, indent int) error {
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
		b.WriteString(quote(v))
	case *node.OrderedMap:
		return writeDict(b, v, indent)
	case []node.Node:
		return writeList(b, v, indent)
	default:
		if node.IsNumeric(n) {
			s, err := node.FormatNumber(n)
			if err != nil {
				return fmt.Errorf("python: %w", err)
			}
			b.WriteString(s)
			return nil
		}
		return fmt.Errorf("%sinvalid node type %T", node.MsgPrefix, n)
	}
	return nil
}

func writeDict(b *strings.Builder, m *node.OrderedMap, indent int) error {
	if m.Len() == 0 {
		b.WriteString("{}")
		return nil
	}
	b.WriteString("{\n")
	i := 0
	for k, v := range m.All() {
		emit.Indent(b, indent+1)
		b.WriteString(quote(k))
		b.WriteString(": ")
		if err := writeValue(b, v, indent+1); err != nil {
			return err
		}
		if i < m.Len()-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
		i++
	}
	emit.Indent(b, indent)
	b.WriteByte('}')
	return nil
}

func writeList(b *strings.Builder, arr []node.Node, indent int) error {
	if len(arr) == 0 {
		b.WriteString("[]")
		return nil
	}
	b.WriteString("[\n")
	for i, e := range arr {
		emit.Indent(b, indent+1)
		if err := writeValue(b, e, indent+1); err != nil {
			return err
		}
		if i < len(arr)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	emit.Indent(b, indent)
	b.WriteByte(']')
	return nil
}

// quote renders s as a double-quoted Python string literal.
func quote(s string) string {
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
