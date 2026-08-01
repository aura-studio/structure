package structure

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/aura-studio/structure/v2/format"
	"github.com/aura-studio/structure/v2/node"
)

func TestSentinelErrorsIs(t *testing.T) {
	if !errors.Is(ErrUnsupportedStructure, errors.ErrUnsupported) {
		t.Error("ErrUnsupportedStructure must match errors.ErrUnsupported")
	}
	for _, e := range []error{ErrTooDeep, ErrTopLevelScalar, ErrDuplicateKey} {
		if !errors.Is(e, e) {
			t.Errorf("errors.Is(%v, itself) = false", e)
		}
	}
	// Wrapping must preserve matching.
	wrapped := fmt.Errorf("encode: %w", ErrTooDeep)
	if !errors.Is(wrapped, ErrTooDeep) {
		t.Error("wrapped ErrTooDeep not matched")
	}
	wrapped2 := fmt.Errorf("context: %w", ErrUnsupportedStructure)
	if !errors.Is(wrapped2, errors.ErrUnsupported) {
		t.Error("wrapped ErrUnsupportedStructure must still match errors.ErrUnsupported")
	}
}

func TestSentinelErrorMessages(t *testing.T) {
	wantSub := map[error]string{
		ErrTooDeep:        "10000",
		ErrTopLevelScalar: "top-level scalar",
		ErrDuplicateKey:   "duplicate",
	}
	for e, sub := range wantSub {
		if got := e.Error(); !strings.Contains(got, sub) {
			t.Errorf("%q does not contain %q", got, sub)
		}
	}
}

// TestSentinelErrorTextIsExact pins the full text of all four sentinels, not
// just a substring. These strings are the public error surface: callers log and
// match them, and the refactor split them across two packages that each own a
// copy of the prefix, so a drift on either side would go unnoticed by a
// substring check.
func TestSentinelErrorTextIsExact(t *testing.T) {
	want := map[error]string{
		ErrUnsupportedStructure: "structure: unsupported node shape for target format: unsupported operation",
		ErrTooDeep:              "structure: nesting depth exceeds 10000",
		ErrTopLevelScalar:       "structure: top-level scalar is not supported (need a mapping or an array)",
		ErrDuplicateKey:         "structure: duplicate mapping key",
	}
	for e, text := range want {
		if got := e.Error(); got != text {
			t.Errorf("sentinel text drifted:\n got %q\nwant %q", got, text)
		}
	}
}

// TestMsgPrefixIsSharedVerbatim pins the two independent copies of the module
// prefix to the same literal. Package node and package format are separate roots
// and neither imports the other, so each declares its own MsgPrefix; Parse folds
// a bare sentinel into a *ParseError by stripping node.MsgPrefix from text that
// either package may have produced. If the two ever diverged, the prefix would
// survive into the message and read "parse json: structure: ...".
func TestMsgPrefixIsSharedVerbatim(t *testing.T) {
	if node.MsgPrefix != format.MsgPrefix {
		t.Fatalf("node.MsgPrefix = %q but format.MsgPrefix = %q", node.MsgPrefix, format.MsgPrefix)
	}
	if node.MsgPrefix != "structure: " {
		t.Fatalf("MsgPrefix = %q, want %q", node.MsgPrefix, "structure: ")
	}
}

// TestFacadeAliasesPreserveTypeIdentity pins that the facade re-exports types
// by alias rather than by definition. A defined type would compile just as well
// but would break every caller that mixes the two spellings: errors.As with
// *format.ParseError would stop matching an error built here, and a type switch
// on node.Node would stop seeing a *structure.OrderedMap.
func TestFacadeAliasesPreserveTypeIdentity(t *testing.T) {
	if got, want := reflect.TypeOf(NewOrderedMap()), reflect.TypeOf(node.NewOrderedMap()); got != want {
		t.Errorf("OrderedMap identity: %v != %v", got, want)
	}
	if got, want := reflect.TypeOf(&ParseError{}), reflect.TypeOf(&format.ParseError{}); got != want {
		t.Errorf("ParseError identity: %v != %v", got, want)
	}

	// A *format.ParseError produced by a codec must be reachable as both
	// spellings, since errors.As is type-identity based.
	_, err := Parse("{", JSON)
	var viaFacade *ParseError
	var viaFormat *format.ParseError
	if !errors.As(err, &viaFacade) {
		t.Fatalf("errors.As(*structure.ParseError) failed for %v", err)
	}
	if !errors.As(err, &viaFormat) {
		t.Fatalf("errors.As(*format.ParseError) failed for %v", err)
	}
	if viaFacade != viaFormat {
		t.Error("the two spellings extracted different pointers")
	}

	// The Format constants must keep the values package format assigns them.
	if JSON != format.JSON || JS != format.JS {
		t.Error("facade Format constants diverge from the format package")
	}
}
