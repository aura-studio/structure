package toml_test

import (
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/aura-studio/structure/v2/codec/toml"
	"github.com/aura-studio/structure/v2/internal/nodetest"
	"github.com/aura-studio/structure/v2/node"
)

func TestTOMLParseOrderAoTAndTime(t *testing.T) {
	input := "z = 1\na = 2\nd = 1979-05-27\nldt = 1979-05-27T07:32:00\nlt = 07:32:00\nodt = 1979-05-27T07:32:00Z\n[[items]]\nid = 1\n[[items]]\nid = 2\n"
	n, err := toml.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	m := n.(*node.OrderedMap)
	if got := strings.Join(m.Keys(), ","); got != "z,a,d,ldt,lt,odt,items" {
		t.Fatalf("order %s", got)
	}
	for k, want := range map[string]string{"d": "1979-05-27", "ldt": "1979-05-27T07:32:00", "lt": "07:32:00", "odt": "1979-05-27T07:32:00Z"} {
		v, _ := m.Get(k)
		if v != want {
			t.Errorf("%s=%v", k, v)
		}
	}
	v, _ := m.Get("items")
	if len(v.([]node.Node)) != 2 {
		t.Fatal(v)
	}
}

func TestTOMLEncodeAndRestrictions(t *testing.T) {
	n := nodetest.OM(
		"title", "x\n",
		"whole", 7.0,
		"special", math.Inf(-1),
		"arr", nodetest.Arr(int64(1), int64(2)),
		"empty", nodetest.OM(),
		"items", nodetest.Arr(nodetest.OM("id", int64(1)), nodetest.OM("id", int64(2))),
	)
	out, err := toml.Encode(n)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`title = "x\n"`, "whole = 7.0", "special = -inf", "[empty]", "[[items]]"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q:\n%s", s, out)
		}
	}
	if back, err := toml.Parse(out); err != nil || !node.Equal(n, back, false) {
		t.Fatalf("round trip: %v\n%s", err, out)
	}
	bad := []node.Node{
		nodetest.Arr(int64(1)),
		nodetest.OM("n", nil),
		nodetest.OM("u", uint64(math.MaxUint64)),
		nodetest.OM("b", new(big.Int).Lsh(big.NewInt(1), 80)),
	}
	for _, v := range bad {
		if _, err := toml.Encode(v); !errors.Is(err, node.ErrUnsupportedStructure) || !errors.Is(err, errors.ErrUnsupported) {
			t.Errorf("%T err=%v", v, err)
		}
	}
}

func TestTOMLSyntaxError(t *testing.T) {
	if _, err := toml.Parse("x = ["); err == nil {
		t.Fatal("invalid TOML accepted")
	}
}
