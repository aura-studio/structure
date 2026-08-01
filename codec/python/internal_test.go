// In-package (package python, not python_test) for the two lexer helpers and the
// writer's defensive arm. Encode's validate() pass makes the "invalid node type"
// branch unreachable from the public API, so it is exercised directly.
package python

import (
	"strings"
	"testing"

	"github.com/aura-studio/structure/v2/node"
)

// badNode is not a legal Node carrier; the writer must reject it.
var badNode node.Node = int(1)

func TestPythonLexerHelpersAndWriterPaths(t *testing.T) {
	// An empty literal has no digits at all, which no caller should produce; the
	// validator rejects it rather than accepting a zero-length number.
	if err := validateNumberLit("", 10); err == nil {
		t.Error("validateNumberLit accepted an empty literal")
	}
	// Underscores are legal only between digits, in every base.
	for _, tc := range []struct {
		lit  string
		base int
		ok   bool
	}{
		{"1_0", 10, true}, {"1f", 16, true}, {"1_7", 8, true}, {"1_1", 2, true},
		{"_1", 10, false}, {"1_", 10, false}, {"1__0", 10, false},
	} {
		err := validateNumberLit(tc.lit, tc.base)
		if (err == nil) != tc.ok {
			t.Errorf("validateNumberLit(%q, %d) = %v, want ok=%v", tc.lit, tc.base, err, tc.ok)
		}
	}
	if lowerASCII('R') != 'r' || lowerASCII('r') != 'r' {
		t.Error("lowerASCII mishandled case")
	}
	// Only ASCII letters are folded; the byte must otherwise pass through, or a
	// prefix check would start matching unrelated bytes.
	if lowerASCII('_') != '_' || lowerASCII('1') != '1' {
		t.Error("lowerASCII altered a non-letter")
	}

	var b strings.Builder
	if err := writeValue(&b, badNode, 0); err == nil {
		t.Error("writeValue accepted an illegal carrier")
	}
}
