// In-package because the cursor encoding is unexported. Its injectivity is the
// reason the array-of-tables replay works at all, so it is pinned directly here
// rather than only through the parse behaviour it produces (see
// TestTOMLCursorKeyIsInjective in the external test package).
package toml

import "testing"

func TestCursorEncodingIsInjective(t *testing.T) {
	if cursorSeg("a")+cursorIdx(0) == cursorSeg("a#0") {
		t.Error("cursor encoding is not injective: seg+idx aliases a literal key")
	}
	if cursorSeg("a")+cursorSeg("b") == cursorSeg("a\x00b") {
		t.Error("cursor encoding is not injective: two segments alias a NUL key")
	}
	if cursorSeg("a1")+cursorIdx(2) == cursorSeg("a")+cursorIdx(12) {
		t.Error("cursor encoding is not injective: digits merge with the index")
	}
	// A length-prefixed segment cannot be confused with a longer one sharing a
	// prefix, which is what makes concatenation unambiguous.
	if cursorSeg("a")+cursorSeg("b") == cursorSeg("ab") {
		t.Error("cursor encoding is not injective: adjacent segments merge")
	}
}
