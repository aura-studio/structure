// In-package (package yaml, not yaml_test) for the error wrapper and the
// defensive arms of toYAML. Encode's validate() pass makes the "invalid node
// type" branches unreachable from the public API, so they are exercised by
// calling the converter directly.
package yaml

import (
	"errors"
	"math/big"
	"testing"

	"github.com/aura-studio/structure/v2/format"
	"github.com/aura-studio/structure/v2/internal/nodetest"
	"github.com/aura-studio/structure/v2/node"
)

// badNode is not a legal Node carrier; the converter must reject it.
var badNode node.Node = int(1)

func TestYAMLRemainingErrorPaths(t *testing.T) {
	for name, input := range map[string]string{
		"broken second document": "a: 1\n---\n[1,\n",
		"tagged sequence":        "a: !foo [1]\n",
		"sequence element":       "a: [!!binary x]\n",
		"merge key":              "<<: {a: 1}\nb: 2\n",
		"custom scalar tag":      "a: !custom x\n",
		"tagged mapping":         "a: !foo {b: 1}\n",
	} {
		if _, err := Parse(input); err == nil {
			t.Errorf("%s: accepted %q", name, input)
		}
	}
	// A bare document marker resolves to null, which is not a container root.
	if _, err := Parse("---"); !errors.Is(err, node.ErrTopLevelScalar) {
		t.Error("--- accepted as a document")
	}
	// wrapErr falls back to a bare *format.ParseError for untyped errors.
	var pe *format.ParseError
	if err := wrapErr(errors.New("boom")); !errors.As(err, &pe) || pe.Format != format.YAML {
		t.Errorf("wrapErr = %v (%T)", err, err)
	}
	// toYAML's defensive arms, at the root and through both container kinds.
	if _, err := toYAML((*big.Int)(nil)); err == nil {
		t.Error("nil *big.Int converted")
	}
	if _, err := toYAML(badNode); err == nil {
		t.Error("illegal carrier converted")
	}
	if _, err := toYAML(nodetest.OM("k", badNode)); err == nil {
		t.Error("illegal map value converted")
	}
	if _, err := toYAML(nodetest.Arr(badNode)); err == nil {
		t.Error("illegal array element converted")
	}
}
