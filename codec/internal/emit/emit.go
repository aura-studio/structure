// Package emit holds small helpers shared by the encoders.
//
// It sits under codec/internal so only the codec packages can import it: these
// are output-formatting details, not part of the data model, and nothing outside
// codec/ has any business with them.
package emit

import "strings"

// Indent writes level units of two-space indentation.
func Indent(b *strings.Builder, level int) {
	for i := 0; i < level; i++ {
		b.WriteString("  ")
	}
}
