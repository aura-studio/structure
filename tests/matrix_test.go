package tests

import (
	"errors"
	stderrors "errors"
	"fmt"
	"testing"
)

// diffNode reports the first differing path between two trees, for readable
// matrix failures.
func diffNode(a, b Node, ordered bool, path string) string {
	if nodeEqual(a, b, ordered) {
		return ""
	}
	am, aok := a.(*OrderedMap)
	bm, bok := b.(*OrderedMap)
	if aok && bok {
		for _, k := range am.Keys() {
			av, _ := am.Get(k)
			bv, hit := bm.Get(k)
			if !hit {
				return path + "." + k + ": missing in target"
			}
			if d := diffNode(av, bv, ordered, path+"."+k); d != "" {
				return d
			}
		}
	}
	aa, aok := a.([]Node)
	ba, bok := b.([]Node)
	if aok && bok && len(aa) == len(ba) {
		for i := range aa {
			if d := diffNode(aa[i], ba[i], ordered, fmt.Sprintf("%s[%d]", path, i)); d != "" {
				return d
			}
		}
	}
	return fmt.Sprintf("%s: source=%v (%T) target=%v (%T)", path, a, a, b, b)
}

func TestConversionMatrix36(t *testing.T) {
	formats := []Format{JSON, YAML, TOML, Lua, Python, JS}
	for _, from := range formats {
		for _, to := range formats {
			from, to := from, to
			t.Run(from.String()+"_to_"+to.String(), func(t *testing.T) {
				fixture := fixtureRichMap()
				input, err := Encode(fixture, from)
				if err != nil {
					t.Fatalf("fixture encode: %v", err)
				}
				source, err := Parse(input, from)
				if err != nil {
					t.Fatalf("source parse: %v", err)
				}
				out, err := Convert(input, from, to)
				if err != nil {
					t.Fatalf("convert: %v", err)
				}
				got, err := Parse(out, to)
				if err != nil {
					t.Fatalf("target parse: %v\n%s", err, out)
				}
				if !nodeEqual(source, got, ordered(to)) {
					t.Fatalf("semantic mismatch at %s\ninput:\n%s\noutput:\n%s",
						diffNode(source, got, ordered(to), "$"), input, out)
				}
			})
		}
	}
}

func TestTopLevelArrayTargetMatrix(t *testing.T) {
	formats := []Format{JSON, YAML, Lua, Python, JS}
	for _, from := range formats {
		input, err := Encode(fixtureArray(), from)
		if err != nil {
			t.Fatal(err)
		}
		// TOML is now the only target that cannot root a document at an array.
		_, err = Convert(input, from, TOML)
		if !stderrors.Is(err, ErrUnsupportedStructure) || !stderrors.Is(err, errors.ErrUnsupported) {
			t.Errorf("%s->%s err=%v", from, TOML, err)
		}
	}
}
