package js

import (
	jsoncodec "github.com/aura-studio/structure/v2/codec/json"
	"github.com/aura-studio/structure/v2/node"
)

// Encode renders a node.Node tree as a JavaScript literal.
//
// Strict JSON text is a valid JS PrimaryExpression (ES2019+; node.JSONEscape's
// U+2028/U+2029 escaping keeps it safe on older engines, where those two are
// line terminators rather than ordinary characters), so the JSON encoder's
// output is reused verbatim. This is the module's only codec-to-codec edge, and
// it is deliberate: a second renderer would be a second place for the escaping
// rules to drift.
func Encode(n node.Node) (string, error) {
	return jsoncodec.Encode(n)
}
