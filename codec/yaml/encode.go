package yaml

import (
	"bytes"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	yamlv3 "gopkg.in/yaml.v3"

	"github.com/aura-studio/structure/v2/node"
)

// Encode renders a node.Node tree as 2-space-indented YAML.
func Encode(n node.Node) (string, error) {
	if err := node.Validate(n); err != nil {
		return "", err
	}
	root, err := toYAML(n)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	enc := yamlv3.NewEncoder(&buf)
	enc.SetIndent(2) // yaml.v3 defaults to 4
	doc := &yamlv3.Node{Kind: yamlv3.DocumentNode, Content: []*yamlv3.Node{root}}
	if err := enc.Encode(doc); err != nil {
		enc.Close()
		return "", fmt.Errorf("yaml: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("yaml: encode: %w", err)
	}
	return buf.String(), nil
}

// toYAML converts a node.Node into a yaml.Node tree preserving key order.
func toYAML(n node.Node) (*yamlv3.Node, error) {
	switch v := n.(type) {
	case nil:
		return &yamlv3.Node{Kind: yamlv3.ScalarNode, Tag: "!!null", Value: "null"}, nil
	case bool:
		return &yamlv3.Node{Kind: yamlv3.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(v)}, nil
	case int64:
		return &yamlv3.Node{Kind: yamlv3.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(v, 10)}, nil
	case uint64:
		return &yamlv3.Node{Kind: yamlv3.ScalarNode, Tag: "!!int", Value: strconv.FormatUint(v, 10)}, nil
	case *big.Int:
		if v == nil {
			return nil, fmt.Errorf("%sinvalid node: nil *big.Int", node.MsgPrefix)
		}
		return &yamlv3.Node{Kind: yamlv3.ScalarNode, Tag: "!!int", Value: v.String()}, nil
	case float64:
		var s string
		switch {
		case math.IsNaN(v):
			s = ".nan"
		case math.IsInf(v, 1):
			s = ".inf"
		case math.IsInf(v, -1):
			s = "-.inf"
		default:
			s = strconv.FormatFloat(v, 'g', -1, 64)
			if !strings.ContainsAny(s, ".eE") {
				s += ".0"
			}
		}
		return &yamlv3.Node{Kind: yamlv3.ScalarNode, Tag: "!!float", Value: s}, nil
	case string:
		sn := &yamlv3.Node{Kind: yamlv3.ScalarNode, Tag: "!!str", Value: v}
		if !plainSafe(v) {
			sn.Style = yamlv3.DoubleQuotedStyle
		}
		return sn, nil
	case *node.OrderedMap:
		m := &yamlv3.Node{Kind: yamlv3.MappingNode, Tag: "!!map"}
		for k, val := range v.All() {
			vn, err := toYAML(val)
			if err != nil {
				return nil, err
			}
			kn := &yamlv3.Node{Kind: yamlv3.ScalarNode, Tag: "!!str", Value: k}
			if !plainSafe(k) {
				kn.Style = yamlv3.DoubleQuotedStyle
			}
			m.Content = append(m.Content, kn, vn)
		}
		return m, nil
	case []node.Node:
		s := &yamlv3.Node{Kind: yamlv3.SequenceNode, Tag: "!!seq"}
		for _, e := range v {
			en, err := toYAML(e)
			if err != nil {
				return nil, err
			}
			s.Content = append(s.Content, en)
		}
		return s, nil
	}
	return nil, fmt.Errorf("%sinvalid node type %T", node.MsgPrefix, n)
}

// plainSafe reports whether s survives a round trip when yaml.v3 is left to
// pick the scalar style itself. Three cases do not, and all three lose data
// silently rather than erroring, so the encoder forces double quotes for them:
//
//   - "<<" resolves to the !!merge tag when emitted plain.
//   - A leading newline is swallowed by the literal block style ("\nx" comes
//     back as "x").
//   - A tab anywhere makes the emitted block unparseable by yaml.v3 itself
//     ("found a tab character where an indentation space is expected").
//
// Derived by enumerating every string up to length 4 over {x \n \r \t space #
// :} and checking which fail; \r needs no help (yaml.v3 quotes it already) but
// is included as cheap insurance.
func plainSafe(s string) bool {
	if s == "<<" {
		return false
	}
	return !strings.HasPrefix(s, "\n") && !strings.ContainsAny(s, "\t\r")
}
