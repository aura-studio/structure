package structure

import (
	"fmt"
	"math/big"
	"regexp"

	gojaast "github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
	"github.com/dop251/goja/token"
	"github.com/dop251/goja/unistring"
)

// jsIntLike matches a plain decimal integer literal; used to recover integers
// that goja's lexer degraded to float64 (it caps integer resolution at int64).
var jsIntLike = regexp.MustCompile(`^[0-9]+$`)

// parseJS decodes a JavaScript object/array literal using goja's pure parser
// (parse only — never RunString/Export, which would execute code including
// getters). The AST is then whitelisted-interpreted directly (Requirement 3.5).
func parseJS(input string) (Node, error) {
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
			return nil, newParseError(JS, pos.Line, col, "%s", el[0].Message)
		}
		return nil, &ParseError{Format: JS, Msg: err.Error(), err: err}
	}
	if len(prog.Body) != 1 {
		return nil, newParseError(JS, 0, 0, "expected exactly one expression")
	}
	exprStat, ok := prog.Body[0].(*gojaast.ExpressionStatement)
	if !ok {
		return nil, newParseError(JS, 0, 0, "expected an expression, got %T", prog.Body[0])
	}
	n, err := jsExprToNode(exprStat.Expression, 1)
	if err != nil {
		return nil, err
	}
	switch n.(type) {
	case *OrderedMap, []Node:
		return n, nil
	default:
		return nil, fmt.Errorf("%w: JavaScript root is %T", ErrTopLevelScalar, n)
	}
}

// jsExprToNode whitelisted-interprets one expression node.
func jsExprToNode(expr gojaast.Expression, depth int) (Node, error) {
	if err := tooDeep(depth); err != nil {
		return nil, err
	}
	switch e := expr.(type) {
	case *gojaast.ObjectLiteral:
		return jsObjectToNode(e, depth)
	case *gojaast.ArrayLiteral:
		arr := make([]Node, len(e.Value))
		for i, el := range e.Value {
			if el == nil {
				arr[i] = nil // elision
				continue
			}
			v, err := jsExprToNode(el, depth+1)
			if err != nil {
				return nil, err
			}
			arr[i] = v
		}
		return arr, nil
	case *gojaast.StringLiteral:
		return jsString(e.Value)
	case *gojaast.NumberLiteral:
		return jsNumberToNode(e)
	case *gojaast.BooleanLiteral:
		return e.Value, nil
	case *gojaast.NullLiteral:
		return nil, nil
	case *gojaast.UnaryExpression:
		if e.Postfix || (e.Operator != token.MINUS && e.Operator != token.PLUS) {
			return nil, newParseError(JS, 0, 0, "unsupported operator in JS literal")
		}
		inner, err := jsExprToNode(e.Operand, depth+1)
		if err != nil {
			return nil, err
		}
		if e.Operator == token.PLUS {
			if !isNumeric(inner) {
				return nil, newParseError(JS, 0, 0, "unary + applied to a non-number")
			}
			return inner, nil
		}
		return negateNumber(JS, inner)
	}
	return nil, newParseError(JS, 0, 0, "unsupported JavaScript expression %T (only literals are allowed)", expr)
}

// jsString decodes a goja string value to UTF-8. goja stores the decoded value
// as a unistring.String: plain UTF-8 while the literal is pure ASCII, but raw
// UTF-16 code units (behind a BOM marker) as soon as any non-ASCII character
// appears. .String() normalizes both forms; a bare string conversion would
// hand back the UTF-16 bytes and corrupt the text. A lone surrogate has no
// UTF-8 encoding at all and utf16.Decode would silently substitute U+FFFD, so
// it is rejected instead.
func jsString(v unistring.String) (string, error) {
	if units := v.AsUtf16(); units != nil {
		for i := 1; i < len(units); i++ { // units[0] is goja's BOM marker
			u := units[i]
			switch {
			case u >= 0xD800 && u <= 0xDBFF: // high surrogate
				if i+1 < len(units) && units[i+1] >= 0xDC00 && units[i+1] <= 0xDFFF {
					i++ // correctly paired; utf16.Decode combines the two
					continue
				}
				return "", newParseError(JS, 0, 0, "string contains a lone surrogate (U+%04X)", u)
			case u >= 0xDC00 && u <= 0xDFFF: // unpaired low surrogate
				return "", newParseError(JS, 0, 0, "string contains a lone surrogate (U+%04X)", u)
			}
		}
	}
	return v.String(), nil
}

// jsNumberToNode maps goja's pre-typed number values onto the carrier ladder.
// The goja lexer yields int64 / float64 / *big.Int.
func jsNumberToNode(e *gojaast.NumberLiteral) (Node, error) {
	switch v := e.Value.(type) {
	case int64:
		return v, nil
	case float64:
		// goja caps integer resolution at int64 and degrades larger integers to
		// float64 (or even +Inf), losing precision. The literal text is still
		// exact, so recover it (Requirement 7.1). Only a plain digit run
		// qualifies: "1.0" and "1e2" are genuinely floats and must stay floats.
		if jsIntLike.MatchString(e.Literal) {
			if b, ok := new(big.Int).SetString(e.Literal, 10); ok {
				return luaBigResult(b), nil
			}
		}
		return v, nil
	case *big.Int:
		return luaBigResult(v), nil
	}
	return nil, newParseError(JS, 0, 0, "unsupported number literal %q", e.Literal)
}

// jsObjectToNode converts an object literal; __proto__ is kept as an ordinary
// own key (JSON.parse semantics) because we never evaluate the literal.
func jsObjectToNode(e *gojaast.ObjectLiteral, depth int) (Node, error) {
	m := NewOrderedMap()
	for _, prop := range e.Value {
		switch pt := prop.(type) {
		case *gojaast.PropertyKeyed:
			if pt.Kind != gojaast.PropertyKindValue {
				return nil, newParseError(JS, 0, 0, "getters/setters are not supported")
			}
			if pt.Computed {
				return nil, newParseError(JS, 0, 0, "computed keys are not supported")
			}
			key, err := jsPropertyKey(pt.Key)
			if err != nil {
				return nil, err
			}
			val, err := jsExprToNode(pt.Value, depth+1)
			if err != nil {
				return nil, err
			}
			m.Set(key, val) // duplicate keys: last write wins (non-strict JS)
		case *gojaast.PropertyShort:
			return nil, newParseError(JS, 0, 0, "shorthand properties are not supported")
		default:
			return nil, newParseError(JS, 0, 0, "unsupported property type %T", prop)
		}
	}
	return m, nil
}

// jsPropertyKey accepts string or numeric keys (identifiers are parsed by
// goja into string/number literals as property keys).
func jsPropertyKey(expr gojaast.Expression) (string, error) {
	switch k := expr.(type) {
	case *gojaast.StringLiteral:
		return jsString(k.Value)
	case *gojaast.NumberLiteral:
		i, ok := k.Value.(int64)
		if !ok {
			return "", newParseError(JS, 0, 0, "unsupported numeric key %q", k.Literal)
		}
		return fmt.Sprintf("%d", i), nil
	}
	return "", newParseError(JS, 0, 0, "unsupported property key %T", expr)
}

// encodeJS renders a Node as a JavaScript literal. Strict JSON text is a
// valid JS PrimaryExpression (ES2019+; Go's U+2028/U+2029 escaping keeps it
// safe on older engines), so the JSON encoder output is reused verbatim.
func encodeJS(n Node) (string, error) {
	return encodeJSON(n)
}
