package structure

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// yamlIntLike matches plain integer text; used to recover big integers that
// yaml.v3 silently degraded to !!float (it caps integer resolution at uint64).
var yamlIntLike = regexp.MustCompile(`^[-+]?[0-9][0-9_]*$`)

// parseYAML decodes a single YAML document into a Node tree via yaml.v3's
// ordered yaml.Node API. Multi-document streams, aliases, anchors, merge keys
// and custom tags are rejected.
func parseYAML(input string) (Node, error) {
	dec := yaml.NewDecoder(strings.NewReader(input))
	var doc yaml.Node
	err := dec.Decode(&doc)
	if err == io.EOF {
		return nil, newParseError(YAML, 0, 0, "empty input")
	}
	if err != nil {
		return nil, wrapYAMLErr(err)
	}
	// Reject multi-document streams: the second Decode must hit EOF.
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, wrapYAMLErr(err)
		}
		return nil, newParseError(YAML, extra.Line, extra.Column, "multiple YAML documents are not supported")
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, newParseError(YAML, doc.Line, doc.Column, "empty YAML document")
	}
	node, err := yamlToNode(doc.Content[0], 1)
	if err != nil {
		return nil, err
	}
	// Root must be a mapping or sequence (design: no bare scalar documents),
	// consistent with the JSON/Python/JS parsers.
	switch node.(type) {
	case *OrderedMap, []Node:
		return node, nil
	}
	return nil, fmt.Errorf("%w: YAML root is %T", ErrTopLevelScalar, node)
}

// yamlToNode converts a yaml.Node subtree into the Node model.
func yamlToNode(n *yaml.Node, depth int) (Node, error) {
	// A scalar is a leaf, not a nesting level. Counting it made the effective
	// YAML limit one level shallower than every other parser's: the same
	// 10000-deep {"a":{"a":...1}} was accepted as JSON and rejected as YAML,
	// because the innermost 1 was charged as level 10001.
	if n.Kind != yaml.ScalarNode {
		if err := tooDeep(depth); err != nil {
			return nil, err
		}
	}
	if n.Anchor != "" {
		return nil, newParseError(YAML, n.Line, n.Column, "anchors are not supported")
	}
	switch n.Kind {
	case yaml.AliasNode:
		return nil, newParseError(YAML, n.Line, n.Column, "aliases are not supported")
	case yaml.MappingNode:
		if n.Tag != "" && n.Tag != "!!map" {
			return nil, newParseError(YAML, n.Line, n.Column, "unsupported tag %q", n.Tag)
		}
		return yamlMappingToNode(n, depth)
	case yaml.SequenceNode:
		if n.Tag != "" && n.Tag != "!!seq" {
			return nil, newParseError(YAML, n.Line, n.Column, "unsupported tag %q", n.Tag)
		}
		arr := make([]Node, 0, len(n.Content))
		for _, item := range n.Content {
			v, err := yamlToNode(item, depth+1)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		return arr, nil
	case yaml.ScalarNode:
		return yamlScalarToNode(n)
	}
	return nil, newParseError(YAML, n.Line, n.Column, "unsupported YAML node kind %d", n.Kind)
}

func yamlMappingToNode(n *yaml.Node, depth int) (Node, error) {
	if len(n.Content)%2 != 0 {
		return nil, newParseError(YAML, n.Line, n.Column, "malformed mapping (odd content)")
	}
	m := NewOrderedMap()
	for i := 0; i < len(n.Content); i += 2 {
		kn, vn := n.Content[i], n.Content[i+1]
		// Only a *plain* << carries merge semantics; yaml.v3 tags it !!merge.
		// A quoted "<<" resolves to !!str and is an ordinary key.
		if kn.Tag == "!!merge" {
			return nil, newParseError(YAML, kn.Line, kn.Column, "merge keys are not supported")
		}
		if kn.Kind != yaml.ScalarNode || kn.Tag != "!!str" {
			return nil, newParseError(YAML, kn.Line, kn.Column, "only string mapping keys are supported")
		}
		key := kn.Value
		if _, dup := m.Get(key); dup {
			return nil, duplicateKeyError(YAML, kn.Line, kn.Column, key)
		}
		val, err := yamlToNode(vn, depth+1)
		if err != nil {
			return nil, err
		}
		m.Set(key, val)
	}
	return m, nil
}

// yamlScalarToNode resolves a scalar by its resolved tag.
func yamlScalarToNode(n *yaml.Node) (Node, error) {
	switch n.Tag {
	case "", "!!str":
		return n.Value, nil
	case "!!null":
		// Only the canonical null spellings may carry the tag; an explicit
		// `!!null x` would otherwise discard "x" silently.
		switch strings.ToLower(n.Value) {
		case "", "~", "null":
			return nil, nil
		}
		return nil, newParseError(YAML, n.Line, n.Column, "invalid null %q", n.Value)
	case "!!bool":
		// yaml.v3 resolves true/True/TRUE (and false variants) to !!bool while
		// keeping the original text; normalize case-insensitively.
		switch strings.ToLower(n.Value) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, newParseError(YAML, n.Line, n.Column, "invalid bool %q", n.Value)
	case "!!int":
		v, err := numberNode(n.Value, 0)
		if err != nil {
			return nil, newParseError(YAML, n.Line, n.Column, "invalid integer %q", n.Value)
		}
		// numberNode falls back to float64; an explicit `!!int 1.5` must not
		// silently become a float.
		if !isIntegerCarrier(v) {
			return nil, newParseError(YAML, n.Line, n.Column, "invalid integer %q", n.Value)
		}
		return v, nil
	case "!!float":
		// yaml.v3 caps integer resolution at uint64 and silently degrades larger
		// integers to !!float with the original text intact — recover those as
		// *big.Int (Requirement 7.2). Only do this for an *implicitly* resolved
		// tag: `!!float 1` written by hand is a float and must stay one.
		if n.Style&yaml.TaggedStyle == 0 && yamlIntLike.MatchString(n.Value) {
			// YAML 1.1 allows underscores as digit separators, and yaml.v3
			// resolves 1_000 as the integer 1000, so they must be stripped here
			// too or the separated spelling silently keeps the degraded float.
			if b, ok := new(big.Int).SetString(strings.ReplaceAll(n.Value, "_", ""), 10); ok {
				// Normalize back onto the int64 -> uint64 -> *big.Int ladder so a
				// recovered value carries the same type as a directly parsed one.
				return luaBigResult(b), nil
			}
		}
		switch strings.ToLower(n.Value) {
		case ".nan":
			return math.NaN(), nil
		case ".inf", "+.inf":
			return math.Inf(1), nil
		case "-.inf":
			return math.Inf(-1), nil
		}
		f, err := strconv.ParseFloat(n.Value, 64)
		if err != nil {
			return nil, newParseError(YAML, n.Line, n.Column, "invalid float %q", n.Value)
		}
		return f, nil
	case "!!timestamp":
		// Requirement 7.4: keep the original text, never a time.Time.
		return n.Value, nil
	case "!!binary":
		return nil, newParseError(YAML, n.Line, n.Column, "!!binary values are not supported")
	default:
		return nil, newParseError(YAML, n.Line, n.Column, "unsupported tag %q", n.Tag)
	}
}

// yamlPlainSafe reports whether s survives a round trip when yaml.v3 is left to
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
func yamlPlainSafe(s string) bool {
	if s == "<<" {
		return false
	}
	return !strings.HasPrefix(s, "\n") && !strings.ContainsAny(s, "\t\r")
}

func wrapYAMLErr(err error) error {
	// yaml.v3 has its own depth cap and reaches it before our counter can fire;
	// map it onto the documented sentinel (Requirement 8.1), as the JSON path
	// does for the standard library's equivalent message.
	if strings.Contains(err.Error(), "exceeded max depth") {
		return ErrTooDeep
	}
	var te *yaml.TypeError
	if errors.As(err, &te) && len(te.Errors) > 0 {
		return newParseError(YAML, 0, 0, "%s", te.Errors[0])
	}
	return &ParseError{Format: YAML, Msg: err.Error(), err: err}
}

// encodeYAML renders a Node tree as 2-space-indented YAML.
func encodeYAML(n Node) (string, error) {
	if err := validate(n); err != nil {
		return "", err
	}
	root, err := nodeToYAML(n)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2) // default is 4; Requirement 6.1 demands 2
	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
	if err := enc.Encode(doc); err != nil {
		enc.Close()
		return "", fmt.Errorf("yaml: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("yaml: encode: %w", err)
	}
	return buf.String(), nil
}

// nodeToYAML converts a Node into a yaml.Node tree preserving key order.
func nodeToYAML(n Node) (*yaml.Node, error) {
	switch v := n.(type) {
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(v)}, nil
	case int64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(v, 10)}, nil
	case uint64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatUint(v, 10)}, nil
	case *big.Int:
		if v == nil {
			return nil, fmt.Errorf("structure: invalid node: nil *big.Int")
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: v.String()}, nil
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
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: s}, nil
	case string:
		sn := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
		if !yamlPlainSafe(v) {
			sn.Style = yaml.DoubleQuotedStyle
		}
		return sn, nil
	case *OrderedMap:
		m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for k, val := range v.All() {
			vn, err := nodeToYAML(val)
			if err != nil {
				return nil, err
			}
			kn := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}
			if !yamlPlainSafe(k) {
				kn.Style = yaml.DoubleQuotedStyle
			}
			m.Content = append(m.Content, kn, vn)
		}
		return m, nil
	case []Node:
		s := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, e := range v {
			en, err := nodeToYAML(e)
			if err != nil {
				return nil, err
			}
			s.Content = append(s.Content, en)
		}
		return s, nil
	}
	return nil, fmt.Errorf("structure: invalid node type %T", n)
}
