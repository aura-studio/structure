package json_test

import (
	"errors"
	"testing"

	"github.com/aura-studio/structure/v2/codec/json"
	"github.com/aura-studio/structure/v2/node"
)

// Error branches the behavioural tests do not reach on their own.

func TestJSONTokenNamesAndScalarRoots(t *testing.T) {
	for _, input := range []string{"1", "true", "null", `"x"`, "1.5"} {
		_, err := json.Parse(input)
		if !errors.Is(err, node.ErrTopLevelScalar) {
			t.Errorf("Parse(%q) err = %v, want ErrTopLevelScalar", input, err)
		}
	}
	for _, input := range []string{`{"a":}`, `{"a":1,}`, `[1,]`, `{1:2}`, `{"a" 1}`, `[`, `{"a":[1`} {
		if _, err := json.Parse(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	// A number too large for float64 still reports a parse error.
	if _, err := json.Parse(`{"a":1e400}`); err == nil {
		t.Error("1e400 accepted")
	}
}
