package json

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/aura-studio/structure/v2/codec/internal/emit"
	"github.com/aura-studio/structure/v2/node"
)

// Encode renders a node.Node tree as indented (2-space) JSON text.
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

// writeValue emits n with the given indentation level. Strings and numbers are
// rendered by node so there is exactly one escaper and one scalar switch in the
// module; only the layout is this package's concern.
func writeValue(b *strings.Builder, n node.Node, indent int) error {
	switch v := n.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(v))
	case string:
		b.WriteString(node.JSONEscape(v))
	case *node.OrderedMap:
		return writeObject(b, v, indent)
	case []node.Node:
		return writeArray(b, v, indent)
	default:
		if node.IsNumeric(n) {
			s, err := node.FormatNumber(n)
			if err != nil {
				return fmt.Errorf("json: %w", err)
			}
			b.WriteString(s)
			return nil
		}
		return fmt.Errorf("%sinvalid node type %T", node.MsgPrefix, n)
	}
	return nil
}

func writeObject(b *strings.Builder, m *node.OrderedMap, indent int) error {
	if m.Len() == 0 {
		b.WriteString("{}")
		return nil
	}
	b.WriteString("{\n")
	i := 0
	for k, val := range m.All() {
		emit.Indent(b, indent+1)
		b.WriteString(node.JSONEscape(k))
		b.WriteString(": ")
		if err := writeValue(b, val, indent+1); err != nil {
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

func writeArray(b *strings.Builder, arr []node.Node, indent int) error {
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
