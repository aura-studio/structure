package tests

import "testing"

// benchInputs holds a representative document per format, produced from the
// shared fixture so all formats carry the same payload.
func benchInputs(b *testing.B) map[Format]string {
	b.Helper()
	inputs := make(map[Format]string)
	for _, f := range allFormats {
		text, err := Encode(fixtureRichMap(), f)
		if err != nil {
			b.Fatalf("bench fixture for %s: %v", f, err)
		}
		inputs[f] = text
	}
	return inputs
}

func BenchmarkParse(b *testing.B) {
	inputs := benchInputs(b)
	for _, f := range allFormats {
		input := inputs[f]
		b.Run(f.String(), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(input)))
			for b.Loop() {
				if _, err := Parse(input, f); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkEncode(b *testing.B) {
	for _, f := range allFormats {
		f := f
		node := Node(fixtureRichMap())
		b.Run(f.String(), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Encode(node, f); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkConvert(b *testing.B) {
	inputs := benchInputs(b)
	routes := []struct{ from, to Format }{
		{JSON, YAML},
		{JSON, Python},
		{YAML, TOML},
		{JSON, JSON},
	}
	for _, route := range routes {
		route := route
		input := inputs[route.from]
		b.Run(route.from.String()+"_to_"+route.to.String(), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(input)))
			for b.Loop() {
				if _, err := Convert(input, route.from, route.to); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
