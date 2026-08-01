package structure

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/arnodel/golua/ast"
	"github.com/arnodel/golua/ops"
	"github.com/arnodel/golua/parsing"
	"github.com/arnodel/golua/scanner"
)

// parseLua decodes a Lua 5.3 table-constructor literal via golua's pure
// parser (no VM, no execution). Only literal expressions are accepted; the
// table must be purely an array part or purely a string-key hash part.
func parseLua(input string) (n Node, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = newParseError(Lua, 0, 0, "parser panic: %v", r)
		}
	}()
	sc := scanner.New("<input>", []byte(input))
	exp, perr := parsing.ParseExp(sc)
	if perr != nil {
		return nil, wrapLuaErr(perr)
	}
	n, err = luaExpToNode(exp, 1)
	if err != nil {
		return nil, err
	}
	switch n.(type) {
	case *OrderedMap, []Node:
		return n, nil
	default:
		return nil, fmt.Errorf("%w: Lua root is %T", ErrTopLevelScalar, n)
	}
}

func wrapLuaErr(err error) error {
	if pe, ok := err.(parsing.Error); ok && pe.Got != nil {
		return newParseError(Lua, pe.Got.Line, pe.Got.Column, "%s", pe.Error())
	}
	return &ParseError{Format: Lua, Msg: err.Error(), err: err}
}

// luaExpToNode whitelisted-interprets a golua expression node.
func luaExpToNode(exp ast.ExpNode, depth int) (Node, error) {
	if err := tooDeep(depth); err != nil {
		return nil, err
	}
	switch e := exp.(type) {
	case ast.TableConstructor:
		return luaTableToNode(e, depth)
	case ast.String:
		return string(e.Val), nil
	case ast.Int:
		if e.Val <= math.MaxInt64 {
			return int64(e.Val), nil
		}
		return e.Val, nil // uint64 beyond int64
	case ast.Float:
		return e.Val, nil
	case ast.Bool:
		return e.Val, nil
	case ast.Nil:
		return nil, nil
	case *ast.UnOp:
		if e.Op != ops.OpNeg {
			return nil, newParseError(Lua, 0, 0, "unsupported operator in Lua literal")
		}
		inner, err := luaExpToNode(e.Operand, depth+1)
		if err != nil {
			return nil, err
		}
		return negateNumber(Lua, inner)
	}
	return nil, newParseError(Lua, 0, 0, "unsupported Lua expression %T (only table literals are allowed)", exp)
}

// negateNumber negates a numeric literal node. Both the Lua and JS parsers
// accept a leading sign, so the error is attributed to whichever format is
// doing the parsing.
func negateNumber(f Format, n Node) (Node, error) {
	switch v := n.(type) {
	case int64:
		b := big.NewInt(v)
		b.Neg(b)
		return luaBigResult(b), nil
	case uint64:
		b := new(big.Int).SetUint64(v)
		b.Neg(b)
		return luaBigResult(b), nil
	case *big.Int:
		b := new(big.Int).Neg(v)
		return luaBigResult(b), nil
	case float64:
		return -v, nil
	}
	return nil, newParseError(f, 0, 0, "unary minus applied to a non-number")
}

// luaBigResult normalizes a big.Int back onto the int64 -> uint64 -> *big.Int
// ladder.
func luaBigResult(b *big.Int) Node {
	if b.IsInt64() {
		return b.Int64()
	}
	if b.IsUint64() {
		return b.Uint64()
	}
	return b
}

// luaTableToNode converts a table constructor, enforcing the pure-array /
// pure-string-key model (Requirement 3.2).
func luaTableToNode(tc ast.TableConstructor, depth int) (Node, error) {
	m := NewOrderedMap()
	arr := []Node{}
	hasArray, hasHash := false, false

	for _, f := range tc.Fields {
		switch k := f.Key.(type) {
		case ast.NoTableKey:
			// Array part entry.
			if hasHash {
				return nil, newParseError(Lua, 0, 0, "mixed array/hash tables are not supported")
			}
			hasArray = true
			if _, isNil := f.Value.(ast.Nil); isNil {
				return nil, newParseError(Lua, 0, 0, "nil is not representable in the array part")
			}
			v, err := luaExpToNode(f.Value, depth+1)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		case ast.String:
			// Hash part entry (bare keys and ["k"]= both parse to String).
			if hasArray {
				return nil, newParseError(Lua, 0, 0, "mixed array/hash tables are not supported")
			}
			hasHash = true
			key := string(k.Val)
			if _, isNil := f.Value.(ast.Nil); isNil {
				// Lua semantics: assigning nil removes the key.
				m.Delete(key)
				continue
			}
			v, err := luaExpToNode(f.Value, depth+1)
			if err != nil {
				return nil, err
			}
			m.Set(key, v) // duplicate keys: last write wins (Lua semantics)
		default:
			return nil, newParseError(Lua, 0, 0, "non-string table keys are not supported")
		}
	}
	if hasArray {
		return arr, nil
	}
	return m, nil
}

// luaReserved lists Lua 5.3 reserved words; they cannot be bare keys.
var luaReserved = map[string]bool{
	"and": true, "break": true, "do": true, "else": true, "elseif": true,
	"end": true, "false": true, "for": true, "function": true, "goto": true,
	"if": true, "in": true, "local": true, "nil": true, "not": true,
	"or": true, "repeat": true, "return": true, "then": true, "true": true,
	"until": true, "while": true,
}

func luaBareKey(k string) bool {
	if k == "" || luaReserved[k] {
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

// encodeLua renders a Node tree as a Lua table literal. Hash-part keys are
// emitted sorted (hash order is semantically void in Lua) for determinism.
func encodeLua(n Node) (string, error) {
	if err := validate(n); err != nil {
		return "", err
	}
	var b strings.Builder
	if err := writeLuaValue(&b, n, 0, false); err != nil {
		return "", err
	}
	b.WriteByte('\n')
	return b.String(), nil
}

func writeLuaValue(b *strings.Builder, n Node, indent int, inArray bool) error {
	switch v := n.(type) {
	case nil:
		if inArray {
			return fmt.Errorf("%w: Lua arrays cannot represent nil elements", ErrUnsupportedStructure)
		}
		b.WriteString("nil")
	case bool:
		b.WriteString(strconv.FormatBool(v))
	case string:
		b.WriteString(luaString(v))
	case *OrderedMap:
		return writeLuaMap(b, v, indent)
	case []Node:
		return writeLuaArray(b, v, indent)
	default:
		if isNumeric(n) {
			if err := luaIntegerFits(n); err != nil {
				return err
			}
			s, err := formatNumber(n)
			if err != nil {
				return fmt.Errorf("lua: %w", err)
			}
			b.WriteString(s)
			return nil
		}
		return fmt.Errorf("structure: invalid node type %T", n)
	}
	return nil
}

func writeLuaMap(b *strings.Builder, m *OrderedMap, indent int) error {
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
		writeIndent(b, indent+1)
		if luaBareKey(k) {
			b.WriteString(k)
		} else {
			b.WriteByte('[')
			b.WriteString(luaString(k))
			b.WriteByte(']')
		}
		b.WriteString(" = ")
		if err := writeLuaValue(b, v, indent+1, false); err != nil {
			return err
		}
		b.WriteString(",\n")
	}
	writeIndent(b, indent)
	b.WriteByte('}')
	return nil
}

func writeLuaArray(b *strings.Builder, arr []Node, indent int) error {
	if len(arr) == 0 {
		// An empty Lua table constructor {} reads back as an empty map, so
		// emitting it would silently change the value's shape. Reject, as the
		// encoder rejects everything else Lua cannot round-trip.
		return fmt.Errorf("%w: Lua cannot distinguish an empty array from an empty map", ErrUnsupportedStructure)
	}
	b.WriteString("{\n")
	for _, e := range arr {
		writeIndent(b, indent+1)
		if err := writeLuaValue(b, e, indent+1, true); err != nil {
			return err
		}
		b.WriteString(",\n")
	}
	writeIndent(b, indent)
	b.WriteByte('}')
	return nil
}

// luaIntegerFits rejects integers a Lua source file cannot carry. Lua 5.3
// integers are int64 (math.maxinteger): a decimal literal that overflows is
// read back as a float by the reference implementation and by golua alike, and
// a hex literal wraps around instead — so emitting the digits would produce a
// literal that reads back as a different value. Rejecting matches how the TOML
// encoder handles its own int64 ceiling.
func luaIntegerFits(n Node) error {
	b, ok := asBigInt(n)
	if !ok {
		return nil // float64: no integer range to check
	}
	if !b.IsInt64() {
		return fmt.Errorf("%w: Lua integers are limited to int64 (got %s)", ErrUnsupportedStructure, b)
	}
	return nil
}

// luaString renders s as a double-quoted Lua string. Control characters use
// decimal \ddd escapes (maximally compatible with Lua 5.1 consumers; \xXX is
// deliberately not emitted).
func luaString(s string) string {
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
