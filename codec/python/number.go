package python

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/aura-studio/structure/v2/node"
)

// parseNumber parses an int or float literal (without sign).
func (p *parser) parseNumber(neg bool) (node.Node, error) {
	start := p.pos
	isFloat := false
	base := 10

	if p.peek() == '0' && p.pos+1 < len(p.src) {
		switch p.src[p.pos+1] {
		case 'x', 'X':
			base = 16
			p.pos += 2
			p.scanDigits(16)
		case 'o', 'O':
			base = 8
			p.pos += 2
			p.scanDigits(8)
		case 'b', 'B':
			base = 2
			p.pos += 2
			p.scanDigits(2)
		}
	}
	if base == 10 {
		p.scanDigits(10)
		if p.pos < len(p.src) && p.src[p.pos] == '.' {
			isFloat = true
			p.pos++
			p.scanDigits(10)
		}
		if p.pos < len(p.src) && (p.src[p.pos] == 'e' || p.src[p.pos] == 'E') {
			isFloat = true
			p.pos++
			if p.pos < len(p.src) && (p.src[p.pos] == '+' || p.src[p.pos] == '-') {
				p.pos++
			}
			p.scanDigits(10)
		}
	}
	// Complex suffix is rejected.
	if p.pos < len(p.src) && (p.src[p.pos] == 'j' || p.src[p.pos] == 'J') {
		return nil, p.errorf("complex numbers are not supported")
	}
	lit := p.src[start:p.pos]
	if err := validateNumberLit(lit, base); err != nil {
		return nil, p.errorf("%s", err.Error())
	}
	clean := strings.ReplaceAll(lit, "_", "")
	if isFloat {
		f, err := strconv.ParseFloat(clean, 64)
		if err != nil {
			return nil, p.errorf("invalid float literal %q", lit)
		}
		if neg {
			f = -f
		}
		return f, nil
	}
	parseBase := base
	if base != 10 {
		// clean still includes 0x/0o/0b; base 0 consumes that prefix.
		parseBase = 0
	}
	if i, err := strconv.ParseInt(clean, parseBase, 64); err == nil {
		if neg {
			i = -i
		}
		return i, nil
	}
	b, ok := new(big.Int).SetString(clean, parseBase)
	if !ok {
		return nil, p.errorf("invalid integer literal %q", lit)
	}
	if neg {
		b.Neg(b)
	}
	return node.BigResult(b), nil
}

// scanDigits advances over digits of the given base plus underscores.
func (p *parser) scanDigits(base int) {
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		ok := false
		switch base {
		case 2:
			ok = c == '0' || c == '1'
		case 8:
			ok = c >= '0' && c <= '7'
		case 10:
			ok = isDigit(c)
		case 16:
			ok = isHexDigit(c)
		}
		if ok || c == '_' {
			p.pos++
			continue
		}
		return
	}
}

// validateNumberLit enforces Python's underscore and leading-zero rules.
func validateNumberLit(lit string, base int) error {
	if lit == "" {
		return fmt.Errorf("invalid number literal")
	}
	// Underscores sit between digits, or directly after a base prefix
	// (PEP 515 permits 0x_1f).
	for i, c := range lit {
		if c != '_' {
			continue
		}
		if i == 0 || i == len(lit)-1 {
			return fmt.Errorf("invalid underscore placement in %q", lit)
		}
		afterBasePrefix := base != 10 && i == 2
		if !afterBasePrefix && !isDigitLike(lit[i-1], base) {
			return fmt.Errorf("invalid underscore placement in %q", lit)
		}
		if !isDigitLike(lit[i+1], base) {
			return fmt.Errorf("invalid underscore placement in %q", lit)
		}
	}
	// No leading zeros in decimal integers — except an all-zero run, which
	// CPython accepts: literal_eval("00") and ("0_000") are both 0, while "01"
	// is a SyntaxError.
	if base == 10 && !strings.ContainsAny(lit, ".eE") {
		core := strings.ReplaceAll(lit, "_", "")
		if len(core) > 1 && core[0] == '0' && strings.Trim(core, "0") != "" {
			return fmt.Errorf("leading zeros are not permitted in %q", lit)
		}
	}
	return nil
}

func isDigitLike(c byte, base int) bool {
	switch base {
	case 2:
		return c == '0' || c == '1'
	case 8:
		return c >= '0' && c <= '7'
	case 16:
		return isHexDigit(c)
	}
	return isDigit(c)
}
