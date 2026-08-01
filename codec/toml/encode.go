package toml

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/aura-studio/structure/v2/codec/internal/emit"
	"github.com/aura-studio/structure/v2/node"
)

// Encode renders a node.Node tree as TOML text with 2-space indentation.
func Encode(n node.Node) (string, error) {
	if err := node.Validate(n); err != nil {
		return "", err
	}
	m, ok := n.(*node.OrderedMap)
	if !ok {
		return "", fmt.Errorf("%w: TOML requires a table at the root (got array)", node.ErrUnsupportedStructure)
	}
	var b strings.Builder
	if err := writeTableBody(&b, m, nil, 0); err != nil {
		return "", err
	}
	out := b.String()
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out, nil
}

// isTableArray reports whether arr is a non-empty array whose elements are all
// mappings — encodable as [[...]] arrays of tables.
func isTableArray(arr []node.Node) bool {
	if len(arr) == 0 {
		return false
	}
	for _, e := range arr {
		if _, ok := e.(*node.OrderedMap); !ok {
			return false
		}
	}
	return true
}

// writeTableBody emits the keyvals of m, then its subtables and arrays of
// tables. path is the section path of m (nil for root); level is the indent.
func writeTableBody(b *strings.Builder, m *node.OrderedMap, path []string, level int) error {
	// Pass 1: simple values (scalars, inline arrays, inline tables).
	for k, v := range m.All() {
		switch tv := v.(type) {
		case *node.OrderedMap:
			continue // subtable, pass 2
		case []node.Node:
			if isTableArray(tv) {
				continue // array of tables, pass 3
			}
		}
		emit.Indent(b, level)
		b.WriteString(renderKey(k))
		b.WriteString(" = ")
		if err := writeValue(b, v); err != nil {
			return err
		}
		b.WriteByte('\n')
	}

	// Pass 2: subtables (headers are always emitted, even for empty tables,
	// so round-trips preserve them).
	for k, v := range m.All() {
		sub, ok := v.(*node.OrderedMap)
		if !ok {
			continue
		}
		full := append(append([]string(nil), path...), k)
		sectionBreak(b)
		b.WriteString("[")
		b.WriteString(renderKeyPath(full))
		b.WriteString("]\n")
		if err := writeTableBody(b, sub, full, level+1); err != nil {
			return err
		}
	}

	// Pass 3: arrays of tables.
	for k, v := range m.All() {
		arr, ok := v.([]node.Node)
		if !ok || !isTableArray(arr) {
			continue
		}
		full := append(append([]string(nil), path...), k)
		for _, e := range arr {
			sectionBreak(b)
			b.WriteString("[[")
			b.WriteString(renderKeyPath(full))
			b.WriteString("]]\n")
			if err := writeTableBody(b, e.(*node.OrderedMap), full, level+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// sectionBreak inserts a blank line before a new section unless the buffer is
// empty or already ends with a blank line.
func sectionBreak(b *strings.Builder) {
	s := b.String()
	if s != "" && !strings.HasSuffix(s, "\n\n") {
		b.WriteByte('\n')
	}
}

// writeValue renders any value in inline position.
func writeValue(b *strings.Builder, n node.Node) error {
	switch v := n.(type) {
	case nil:
		return fmt.Errorf("%w: TOML has no null value", node.ErrUnsupportedStructure)
	case bool:
		b.WriteString(strconv.FormatBool(v))
	case string:
		b.WriteString(basicString(v))
	case int64:
		b.WriteString(strconv.FormatInt(v, 10))
	case uint64:
		if v > math.MaxInt64 {
			return fmt.Errorf("%w: TOML integers are limited to int64 (got %d)", node.ErrUnsupportedStructure, v)
		}
		b.WriteString(strconv.FormatUint(v, 10))
	case *big.Int:
		if v == nil {
			return fmt.Errorf("%sinvalid node: nil *big.Int", node.MsgPrefix)
		}
		if !v.IsInt64() {
			return fmt.Errorf("%w: TOML integers are limited to int64 (got %s)", node.ErrUnsupportedStructure, v.String())
		}
		b.WriteString(v.String())
	case float64:
		switch {
		case math.IsNaN(v):
			b.WriteString("nan")
		case math.IsInf(v, 1):
			b.WriteString("inf")
		case math.IsInf(v, -1):
			b.WriteString("-inf")
		default:
			s, err := node.FormatFloat(v)
			if err != nil {
				return err
			}
			b.WriteString(s)
		}
	case *node.OrderedMap:
		return writeInlineTable(b, v)
	case []node.Node:
		return writeInlineArray(b, v)
	default:
		return fmt.Errorf("%sinvalid node type %T", node.MsgPrefix, n)
	}
	return nil
}

func writeInlineTable(b *strings.Builder, m *node.OrderedMap) error {
	b.WriteString("{")
	first := true
	for k, v := range m.All() {
		if !first {
			b.WriteString(", ")
		}
		first = false
		b.WriteString(renderKey(k))
		b.WriteString(" = ")
		if err := writeValue(b, v); err != nil {
			return err
		}
	}
	b.WriteString("}")
	return nil
}

func writeInlineArray(b *strings.Builder, arr []node.Node) error {
	b.WriteString("[")
	for i, e := range arr {
		if i > 0 {
			b.WriteString(", ")
		}
		if err := writeValue(b, e); err != nil {
			return err
		}
	}
	b.WriteString("]")
	return nil
}

// renderKey renders a bare key when possible, else a quoted key.
func renderKey(k string) string {
	if k != "" && isBareKey(k) {
		return k
	}
	return basicString(k)
}

func isBareKey(k string) bool {
	for _, r := range k {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func renderKeyPath(segments []string) string {
	parts := make([]string, len(segments))
	for i, s := range segments {
		parts[i] = renderKey(s)
	}
	return strings.Join(parts, ".")
}

// basicString renders s as a TOML basic string with minimal escapes.
func basicString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
