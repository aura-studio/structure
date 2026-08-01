package lua

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/aura-studio/structure/v2/codec/internal/emit"
	"github.com/aura-studio/structure/v2/node"
)

// reserved lists Lua 5.3 reserved words; they cannot be bare keys.
var reserved = map[string]bool{
	"and": true, "break": true, "do": true, "else": true, "elseif": true,
	"end": true, "false": true, "for": true, "function": true, "goto": true,
	"if": true, "in": true, "local": true, "nil": true, "not": true,
	"or": true, "repeat": true, "return": true, "then": true, "true": true,
	"until": true, "while": true,
}

func bareKey(k string) bool {
	if k == "" || reserved[k] {
		return false
	}
	for i, r := range k {
		switch {
		case r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// Encode renders a node.Node tree as a Lua table literal. Hash-part keys are
// emitted sorted (hash order is semantically void in Lua) for determinism.
func Encode(n node.Node) (string, error) {
	if err := node.Validate(n); err != nil {
		return "", err
	}
	var b strings.Builder
	if err := writeValue(&b, n, 0, false); err != nil {
		return "", err
	}
	b.WriteByte('\n')
	return b.String(), nil
}

func writeValue(b *strings.Builder, n node.Node, indent int, inArray bool) error {
	switch v := n.(type) {
	case nil:
		if inArray {
			return fmt.Errorf("%w: Lua arrays cannot represent nil elements", node.ErrUnsupportedStructure)
		}
		b.WriteString("nil")
	case bool:
		b.WriteString(strconv.FormatBool(v))
	case string:
		b.WriteString(quote(v))
	case *node.OrderedMap:
		return writeMap(b, v, indent)
	case []node.Node:
		return writeArray(b, v, indent)
	default:
		if node.IsNumeric(n) {
			if err := integerFits(n); err != nil {
				return err
			}
			s, err := node.FormatNumber(n)
			if err != nil {
				return fmt.Errorf("lua: %w", err)
			}
			b.WriteString(s)
			return nil
		}
		return fmt.Errorf("%sinvalid node type %T", node.MsgPrefix, n)
	}
	return nil
}

func writeMap(b *strings.Builder, m *node.OrderedMap, indent int) error {
	keys := m.Keys()
	sort.Strings(keys) // deterministic: Lua hash order is unspecified
	// Drop nil values first (Lua: assigning nil deletes a key).
	var emitted []string
	for _, k := range keys {
		v, _ := m.Get(k)
		if v == nil {
			continue
		}
		emitted = append(emitted, k)
	}
	if len(emitted) == 0 {
		b.WriteString("{}")
		return nil
	}
	b.WriteString("{\n")
	for _, k := range emitted {
		v, _ := m.Get(k)
		emit.Indent(b, indent+1)
		if bareKey(k) {
			b.WriteString(k)
		} else {
			b.WriteByte('[')
			b.WriteString(quote(k))
			b.WriteByte(']')
		}
		b.WriteString(" = ")
		if err := writeValue(b, v, indent+1, false); err != nil {
			return err
		}
		b.WriteString(",\n")
	}
	emit.Indent(b, indent)
	b.WriteByte('}')
	return nil
}

func writeArray(b *strings.Builder, arr []node.Node, indent int) error {
	if len(arr) == 0 {
		// An empty Lua table constructor {} reads back as an empty map, so
		// emitting it would silently change the value's shape. Reject, as the
		// encoder rejects everything else Lua cannot round-trip.
		return fmt.Errorf("%w: Lua cannot distinguish an empty array from an empty map", node.ErrUnsupportedStructure)
	}
	b.WriteString("{\n")
	for _, e := range arr {
		emit.Indent(b, indent+1)
		if err := writeValue(b, e, indent+1, true); err != nil {
			return err
		}
		b.WriteString(",\n")
	}
	emit.Indent(b, indent)
	b.WriteByte('}')
	return nil
}

// integerFits rejects integers a Lua source file cannot carry. Lua 5.3 integers
// are int64 (math.maxinteger): a decimal literal that overflows is read back as
// a float by the reference implementation and by golua alike, and a hex literal
// wraps around instead — so emitting the digits would produce a literal that
// reads back as a different value. Rejecting matches how the TOML encoder
// handles its own int64 ceiling.
func integerFits(n node.Node) error {
	b, ok := node.AsBigInt(n)
	if !ok {
		return nil // float64: no integer range to check
	}
	if !b.IsInt64() {
		return fmt.Errorf("%w: Lua integers are limited to int64 (got %s)", node.ErrUnsupportedStructure, b)
	}
	return nil
}

// quote renders s as a double-quoted Lua string. Control characters use decimal
// \ddd escapes (maximally compatible with Lua 5.1 consumers; \xXX is
// deliberately not emitted).
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
			// Decimal \ddd escapes consume up to three digits, so NUL and the
			// other control characters must always take the full three-digit
			// form: "\0" followed by "5" would read back as byte 5.
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, "\\%03d", r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
