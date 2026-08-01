// Package lua parses and encodes Lua 5.3 table-constructor literals as a
// node.Node tree.
//
// Parsing uses golua's pure parser — no VM, no execution — and accepts literal
// expressions only. A table must be purely an array part or purely a string-key
// hash part; mixed tables are rejected.
package lua

import (
	"fmt"
	"math"

	"github.com/arnodel/golua/ast"
	"github.com/arnodel/golua/ops"
	"github.com/arnodel/golua/parsing"
	"github.com/arnodel/golua/scanner"

	"github.com/aura-studio/structure/v2/format"
	"github.com/aura-studio/structure/v2/node"
)

// Parse decodes a Lua table-constructor literal into a node.Node tree.
func Parse(input string) (n node.Node, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = format.New(format.Lua, 0, 0, "parser panic: %v", r)
		}
	}()
	sc := scanner.New("<input>", []byte(input))
	exp, perr := parsing.ParseExp(sc)
	if perr != nil {
		return nil, wrapErr(perr)
	}
	n, err = expToNode(exp, 1)
	if err != nil {
		return nil, err
	}
	switch n.(type) {
	case *node.OrderedMap, []node.Node:
		return n, nil
	default:
		return nil, fmt.Errorf("%w: Lua root is %T", node.ErrTopLevelScalar, n)
	}
}

func wrapErr(err error) error {
	if pe, ok := err.(parsing.Error); ok && pe.Got != nil {
		return format.New(format.Lua, pe.Got.Line, pe.Got.Column, "%s", pe.Error())
	}
	return format.Wrap(format.Lua, 0, 0, err, err.Error())
}

// expToNode whitelisted-interprets a golua expression node.
func expToNode(exp ast.ExpNode, depth int) (node.Node, error) {
	if err := node.TooDeep(depth); err != nil {
		return nil, err
	}
	switch e := exp.(type) {
	case ast.TableConstructor:
		return tableToNode(e, depth)
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
			return nil, format.New(format.Lua, 0, 0, "unsupported operator in Lua literal")
		}
		inner, err := expToNode(e.Operand, depth+1)
		if err != nil {
			return nil, err
		}
		neg, ok := node.Negate(inner)
		if !ok {
			return nil, format.New(format.Lua, 0, 0, "unary minus applied to a non-number")
		}
		return neg, nil
	}
	return nil, format.New(format.Lua, 0, 0, "unsupported Lua expression %T (only table literals are allowed)", exp)
}

// tableToNode converts a table constructor, enforcing the pure-array /
// pure-string-key model.
func tableToNode(tc ast.TableConstructor, depth int) (node.Node, error) {
	m := node.NewOrderedMap()
	arr := []node.Node{}
	hasArray, hasHash := false, false

	for _, f := range tc.Fields {
		switch k := f.Key.(type) {
		case ast.NoTableKey:
			// Array part entry.
			if hasHash {
				return nil, format.New(format.Lua, 0, 0, "mixed array/hash tables are not supported")
			}
			hasArray = true
			if _, isNil := f.Value.(ast.Nil); isNil {
				return nil, format.New(format.Lua, 0, 0, "nil is not representable in the array part")
			}
			v, err := expToNode(f.Value, depth+1)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		case ast.String:
			// Hash part entry (bare keys and ["k"]= both parse to String).
			if hasArray {
				return nil, format.New(format.Lua, 0, 0, "mixed array/hash tables are not supported")
			}
			hasHash = true
			key := string(k.Val)
			if _, isNil := f.Value.(ast.Nil); isNil {
				// Lua semantics: assigning nil removes the key.
				m.Delete(key)
				continue
			}
			v, err := expToNode(f.Value, depth+1)
			if err != nil {
				return nil, err
			}
			m.Set(key, v) // duplicate keys: last write wins (Lua semantics)
		default:
			return nil, format.New(format.Lua, 0, 0, "non-string table keys are not supported")
		}
	}
	if hasArray {
		return arr, nil
	}
	return m, nil
}
