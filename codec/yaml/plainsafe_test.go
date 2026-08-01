// In-package because plainSafe is unexported. Its three exceptions are the
// reason TestYAMLStringStylesRoundTrip passes, so the predicate is pinned
// directly as well: a future simplification that drops one of the cases would
// otherwise only show up as a data-losing round trip.
package yaml

import "testing"

func TestPlainSafeRejectsDataLosingStyles(t *testing.T) {
	for _, s := range []string{"<<", "\nx", "\n", "a\tb", "\ttab", "a\rb"} {
		if plainSafe(s) {
			t.Errorf("plainSafe(%q) = true, want false", s)
		}
	}
	for _, s := range []string{"plain", "", "a: b", "- x", "#c", "~", "true", "x\n", " lead", "trail "} {
		if !plainSafe(s) {
			t.Errorf("plainSafe(%q) = false, want true (yaml.v3 quotes it correctly on its own)", s)
		}
	}
}
