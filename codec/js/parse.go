// Package js parses and encodes JavaScript object/array literals as a
// node.Node tree.
//
// Parsing uses goja's pure parser — parse only, never RunString/Export, which
// would execute code including getters — and the AST is then
// whitelisted-interpreted directly.
package js

import (
	"fmt"
	"math/big"
	"regexp"

	gojaast "github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
	"github.com/dop251/goja/token"
	"github.com/dop251/goja/unistring"

	"github.com/aura-studio/structure/v2/format"
	"github.com/aura-studio/structure/v2/node"
)

// intLike matches a plain decimal integer literal; used to recover integers
// that goja's lexer degraded to float64 (it caps integer resolution at int64).
var intLike = regexp.MustCompile(`^[0-9]+$`)

// Parse decodes a JavaScript object/array literal into a node.Node tree.
func Parse(input string) (node.Node, error) {
	// Parenthesizing forces `{a: 1}` to parse as an object expression rather
	// than a block statement.
	prog, err := parser.ParseFile(nil, "<input>", "("+input+")", 0)
	if err != nil {
		// goja reports the position of each syntax error. Only the first line is
		// shifted by the parenthesis prepended above; later lines are unaffected.
		if el, ok := err.(parser.ErrorList); ok && len(el) > 0 {
			pos := el[0].Position
			col := pos.Column
			if pos.Line == 1 && col > 1 {
				col--
			}
			return nil, format.New(format.JS, pos.Line, col, "%s", el[0].Message)
		}
		return nil, format.Wrap(format.JS, 0, 0, err, err.Error())
	}
	if len(prog.Body) != 1 {
		return nil, format.New(format.JS, 0, 0, "expected exactly one expression")
	}
	exprStat, ok := prog.Body[0].(*gojaast.ExpressionStatement)
	if !ok {
		return nil, format.New(format.JS, 0, 0, "expected an expression, got %T", prog.Body[0])
	}
	n, err := exprToNode(exprStat.Expression, 1)
	if err != nil {
		return nil, err
	}
	switch n.(type) {
	case *node.OrderedMap, []node.Node:
		return n, nil
	default:
		return nil, fmt.Errorf("%w: JavaScript root is %T", node.ErrTopLevelScalar, n)
	}
}

// exprToNode whitelisted-interprets one expression node.
func exprToNode(expr gojaast.Expression, depth int) (node.Node, error) {
	if err := node.TooDeep(depth); err != nil {
		return nil, err
	}
	switch e := expr.(type) {
	case *gojaast.ObjectLiteral:
		return objectToNode(e, depth)
	case *gojaast.ArrayLiteral:
		arr := make([]node.Node, len(e.Value))
		for i, el := range e.Value {
			if el == nil {
				arr[i] = nil // elision
				continue
			}
			v, err := exprToNode(el, depth+1)
			if err != nil {
				return nil, err
			}
			arr[i] = v
		}
		return arr, nil
	case *gojaast.StringLiteral:
		return decodeString(e.Value)
	case *gojaast.NumberLiteral:
		return numberToNode(e)
	case *gojaast.BooleanLiteral:
		return e.Value, nil
	case *gojaast.NullLiteral:
		return nil, nil
	case *gojaast.UnaryExpression:
		if e.Postfix || (e.Operator != token.MINUS && e.Operator != token.PLUS) {
			return nil, format.New(format.JS, 0, 0, "unsupported operator in JS literal")
		}
		inner, err := exprToNode(e.Operand, depth+1)
		if err != nil {
			return nil, err
		}
		if e.Operator == token.PLUS {
			if !node.IsNumeric(inner) {
				return nil, format.New(format.JS, 0, 0, "unary + applied to a non-number")
			}
			return inner, nil
		}
		neg, ok := node.Negate(inner)
		if !ok {
			return nil, format.New(format.JS, 0, 0, "unary minus applied to a non-number")
		}
		return neg, nil
	}
	return nil, format.New(format.JS, 0, 0, "unsupported JavaScript expression %T (only literals are allowed)", expr)
}

// decodeString decodes a goja string value to UTF-8. goja stores the decoded
// value as a unistring.String: plain UTF-8 while the literal is pure ASCII, but
// raw UTF-16 code units (behind a BOM marker) as soon as any non-ASCII character
// appears. .String() normalizes both forms; a bare string conversion would hand
// back the UTF-16 bytes and corrupt the text. A lone surrogate has no UTF-8
// encoding at all and utf16.Decode would silently substitute U+FFFD, so it is
// rejected instead.
func decodeString(v unistring.String) (string, error) {
	if units := v.AsUtf16(); units != nil {
		for i := 1; i < len(units); i++ { // units[0] is goja's BOM marker
			u := units[i]
			switch {
			case u >= 0xD800 && u <= 0xDBFF: // high surrogate
				if i+1 < len(units) && units[i+1] >= 0xDC00 && units[i+1] <= 0xDFFF {
					i++ // correctly paired; utf16.Decode combines the two
					continue
				}
				return "", format.New(format.JS, 0, 0, "string contains a lone surrogate (U+%04X)", u)
			case u >= 0xDC00 && u <= 0xDFFF: // unpaired low surrogate
				return "", format.New(format.JS, 0, 0, "string contains a lone surrogate (U+%04X)", u)
			}
		}
	}
	return v.String(), nil
}

// numberToNode maps goja's pre-typed number values onto the carrier ladder.
// The goja lexer yields int64 / float64 / *big.Int.
func numberToNode(e *gojaast.NumberLiteral) (node.Node, error) {
	switch v := e.Value.(type) {
	case int64:
		return v, nil
	case float64:
		// goja caps integer resolution at int64 and degrades larger integers to
		// float64 (or even +Inf), losing precision. The literal text is still
		// exact, so recover it. Only a plain digit run qualifies: "1.0" and "1e2"
		// are genuinely floats and must stay floats.
		if intLike.MatchString(e.Literal) {
			if b, ok := new(big.Int).SetString(e.Literal, 10); ok {
				return node.BigResult(b), nil
			}
		}
		return v, nil
	case *big.Int:
		return node.BigResult(v), nil
	}
	return nil, format.New(format.JS, 0, 0, "unsupported number literal %q", e.Literal)
}

// objectToNode converts an object literal; __proto__ is kept as an ordinary own
// key (JSON.parse semantics) because we never evaluate the literal.
func objectToNode(e *gojaast.ObjectLiteral, depth int) (node.Node, error) {
	m := node.NewOrderedMap()
	for _, prop := range e.Value {
		switch pt := prop.(type) {
		case *gojaast.PropertyKeyed:
			if pt.Kind != gojaast.PropertyKindValue {
				return nil, format.New(format.JS, 0, 0, "getters/setters are not supported")
			}
			if pt.Computed {
				return nil, format.New(format.JS, 0, 0, "computed keys are not supported")
			}
			key, err := propertyKey(pt.Key)
			if err != nil {
				return nil, err
			}
			val, err := exprToNode(pt.Value, depth+1)
			if err != nil {
				return nil, err
			}
			m.Set(key, val) // duplicate keys: last write wins (non-strict JS)
		case *gojaast.PropertyShort:
			return nil, format.New(format.JS, 0, 0, "shorthand properties are not supported")
		default:
			return nil, format.New(format.JS, 0, 0, "unsupported property type %T", prop)
		}
	}
	return m, nil
}

// propertyKey accepts string or numeric keys (identifiers are parsed by goja
// into string/number literals as property keys).
func propertyKey(expr gojaast.Expression) (string, error) {
	switch k := expr.(type) {
	case *gojaast.StringLiteral:
		return decodeString(k.Value)
	case *gojaast.NumberLiteral:
		i, ok := k.Value.(int64)
		if !ok {
			return "", format.New(format.JS, 0, 0, "unsupported numeric key %q", k.Literal)
		}
		return fmt.Sprintf("%d", i), nil
	}
	return "", format.New(format.JS, 0, 0, "unsupported property key %T", expr)
}
