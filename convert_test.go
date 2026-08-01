package structure

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
