package tests

import (
	"errors"
	"sync"
	"testing"
)

func TestConvertDispatchAndConcurrency(t *testing.T) {
	input := `{"a":1,"b":[true,"x"]}`
	for _, target := range []Format{JSON, YAML, TOML, Lua, Python, JS} {
		out, err := Convert(input, JSON, target)
		if err != nil {
			t.Fatalf("JSON->%s: %v", target, err)
		}
		if _, err := Parse(out, target); err != nil {
			t.Fatalf("parse %s output: %v\n%s", target, err, out)
		}
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := Convert(input, JSON, YAML); errCh <- err }()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
}

// ParseFormat is the facade's only string-to-Format entry point, and it had no
// test at all until the move surfaced it as the one facade function at 0%
// coverage. The alias matrix itself is pinned in package format; what belongs
// here is that the forwarder is wired to it and hands back a value the facade's
// own Parse accepts, which is the whole reason it returns Format and not an int.
func TestParseFormatForwardsAndFeedsParse(t *testing.T) {
	f, err := ParseFormat("YML")
	if err != nil {
		t.Fatalf("ParseFormat(YML): %v", err)
	}
	if f != YAML {
		t.Fatalf("ParseFormat(YML) = %v, want yaml", f)
	}
	if _, err := Parse("a: 1\n", f); err != nil {
		t.Fatalf("Parse with the resolved format: %v", err)
	}
	if _, err := ParseFormat("csv"); err == nil {
		t.Error("ParseFormat accepted an unknown name")
	}
}

func TestInvalidFormatAndNode(t *testing.T) {
	bad := Format(99)
	if _, err := Parse("{}", bad); err == nil {
		t.Fatal("Parse accepted invalid format")
	}
	if _, err := Encode(om(), bad); err == nil {
		t.Fatal("Encode accepted invalid format")
	}
	if _, err := Convert("{}", bad, JSON); err == nil {
		t.Fatal("Convert accepted invalid source")
	}
	if _, err := Encode(map[string]any{"x": 1}, JSON); err == nil {
		t.Fatal("bare map accepted")
	}
	if _, err := Encode(int64(1), JSON); !errors.Is(err, ErrTopLevelScalar) {
		t.Fatal(err)
	}
}
