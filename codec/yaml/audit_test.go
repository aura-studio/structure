package yaml_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/aura-studio/structure/v2/codec/yaml"
	"github.com/aura-studio/structure/v2/format"
)

// A duplicate key used to surface as a bare sentinel with no position. It now
// carries the offending location while staying reachable via errors.Is.
func TestYAMLDuplicateKeyCarriesPosition(t *testing.T) {
	var pe *format.ParseError
	err := func() error { _, err := yaml.Parse("a: 1\na: 2\n"); return err }()
	if !errors.Is(err, format.ErrDuplicateKey) {
		t.Fatalf("not ErrDuplicateKey: %v", err)
	}
	if !errors.As(err, &pe) {
		t.Fatalf("err is %T, want *format.ParseError wrapping the sentinel", err)
	}
	if pe.Line != 2 || pe.Column < 1 {
		t.Errorf("position = line %d col %d, want line 2 col >= 1", pe.Line, pe.Column)
	}
	if !strings.Contains(pe.Error(), "duplicate") {
		t.Errorf("error text %q lacks 'duplicate'", pe.Error())
	}
}

// yaml.v3 implements YAML 1.1 only and rejects a %YAML 1.2 directive. The
// library deliberately surfaces that instead of stripping the directive and
// parsing under 1.1 rules, which would silently change how y/no and 0o777-style
// scalars resolve. Documented as a known limitation, so pin the behaviour.
func TestYAMLVersionDirective(t *testing.T) {
	if _, err := yaml.Parse("%YAML 1.1\n---\na: 1\n"); err != nil {
		t.Errorf("1.1 directive rejected: %v", err)
	}
	for _, in := range []string{"%YAML 1.2\n---\na: 1\n", "%YAML 1.3\n---\na: 1\n"} {
		var pe *format.ParseError
		_, err := yaml.Parse(in)
		if !errors.As(err, &pe) {
			t.Errorf("%q: want a *format.ParseError, got %T (%v)", in, err, err)
			continue
		}
		if pe.Format != format.YAML {
			t.Errorf("%q: Format = %v, want YAML", in, pe.Format)
		}
	}
}
