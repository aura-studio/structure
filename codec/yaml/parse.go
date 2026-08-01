// Package yaml parses and encodes YAML text as a node.Node tree.
//
// Only a single document is supported; aliases, anchors, merge keys and custom
// tags are rejected, and timestamps stay as their original text rather than
// becoming time.Time.
package yaml

import (
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	yamlv3 "gopkg.in/yaml.v3"

	"github.com/aura-studio/structure/v2/format"
	"github.com/aura-studio/structure/v2/node"
)

// intLike matches plain integer text; used to recover big integers that yaml.v3
// silently degraded to !!float (it caps integer resolution at uint64).
var intLike = regexp.MustCompile(`^[-+]?[0-9][0-9_]*$`)

// Parse decodes a single YAML document into a node.Node tree via yaml.v3's
// ordered yaml.Node API. Multi-document streams, aliases, anchors, merge keys
// and custom tags are rejected.
func Parse(input string) (node.Node, error) {
	dec := yamlv3.NewDecoder(strings.NewReader(input))
	var doc yamlv3.Node
	err := dec.Decode(&doc)
	if err == io.EOF {
		return nil, format.New(format.YAML, 0, 0, "empty input")
	}
	if err != nil {
		return nil, wrapErr(err)
	}
	// Reject multi-document streams: the second Decode must hit EOF.
	var extra yamlv3.Node
	if err := dec.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, wrapErr(err)
		}
		return nil, format.New(format.YAML, extra.Line, extra.Column, "multiple YAML documents are not supported")
	}
	if doc.Kind != yamlv3.DocumentNode || len(doc.Content) == 0 {
		return nil, format.New(format.YAML, doc.Line, doc.Column, "empty YAML document")
	}
	root, err := toNode(doc.Content[0], 1)
	if err != nil {
		return nil, err
	}
	// Root must be a mapping or sequence (design: no bare scalar documents),
	// consistent with the JSON/Python/JS parsers.
	switch root.(type) {
	case *node.OrderedMap, []node.Node:
		return root, nil
	}
	return nil, fmt.Errorf("%w: YAML root is %T", node.ErrTopLevelScalar, root)
}

// toNode converts a yaml.Node subtree into the node.Node model.
func toNode(n *yamlv3.Node, depth int) (node.Node, error) {
	// A scalar is a leaf, not a nesting level. Counting it made the effective
	// YAML limit one level shallower than every other parser's: the same
	// 10000-deep {"a":{"a":...1}} was accepted as JSON and rejected as YAML,
	// because the innermost 1 was charged as level 10001.
	if n.Kind != yamlv3.ScalarNode {
		if err := node.TooDeep(depth); err != nil {
			return nil, err
		}
	}
	if n.Anchor != "" {
		return nil, format.New(format.YAML, n.Line, n.Column, "anchors are not supported")
	}
	switch n.Kind {
	case yamlv3.AliasNode:
		return nil, format.New(format.YAML, n.Line, n.Column, "aliases are not supported")
	case yamlv3.MappingNode:
		if n.Tag != "" && n.Tag != "!!map" {
			return nil, format.New(format.YAML, n.Line, n.Column, "unsupported tag %q", n.Tag)
		}
		return mappingToNode(n, depth)
	case yamlv3.SequenceNode:
		if n.Tag != "" && n.Tag != "!!seq" {
			return nil, format.New(format.YAML, n.Line, n.Column, "unsupported tag %q", n.Tag)
		}
		arr := make([]node.Node, 0, len(n.Content))
		for _, item := range n.Content {
			v, err := toNode(item, depth+1)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		return arr, nil
	case yamlv3.ScalarNode:
		return scalarToNode(n)
	}
	return nil, format.New(format.YAML, n.Line, n.Column, "unsupported YAML node kind %d", n.Kind)
}

func mappingToNode(n *yamlv3.Node, depth int) (node.Node, error) {
	if len(n.Content)%2 != 0 {
		return nil, format.New(format.YAML, n.Line, n.Column, "malformed mapping (odd content)")
	}
	m := node.NewOrderedMap()
	for i := 0; i < len(n.Content); i += 2 {
		kn, vn := n.Content[i], n.Content[i+1]
		// Only a *plain* << carries merge semantics; yaml.v3 tags it !!merge.
		// A quoted "<<" resolves to !!str and is an ordinary key.
		if kn.Tag == "!!merge" {
			return nil, format.New(format.YAML, kn.Line, kn.Column, "merge keys are not supported")
		}
		if kn.Kind != yamlv3.ScalarNode || kn.Tag != "!!str" {
			return nil, format.New(format.YAML, kn.Line, kn.Column, "only string mapping keys are supported")
		}
		key := kn.Value
		if _, dup := m.Get(key); dup {
			return nil, format.DuplicateKey(format.YAML, kn.Line, kn.Column, key)
		}
		val, err := toNode(vn, depth+1)
		if err != nil {
			return nil, err
		}
		m.Set(key, val)
	}
	return m, nil
}

// scalarToNode resolves a scalar by its resolved tag.
func scalarToNode(n *yamlv3.Node) (node.Node, error) {
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
		return nil, format.New(format.YAML, n.Line, n.Column, "invalid null %q", n.Value)
	case "!!bool":
		// yaml.v3 resolves true/True/TRUE (and false variants) to !!bool while
		// keeping the original text; normalize case-insensitively.
		switch strings.ToLower(n.Value) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, format.New(format.YAML, n.Line, n.Column, "invalid bool %q", n.Value)
	case "!!int":
		v, err := node.NumberNode(n.Value, 0)
		if err != nil {
			return nil, format.New(format.YAML, n.Line, n.Column, "invalid integer %q", n.Value)
		}
		// NumberNode falls back to float64; an explicit `!!int 1.5` must not
		// silently become a float.
		if !node.IsIntegerCarrier(v) {
			return nil, format.New(format.YAML, n.Line, n.Column, "invalid integer %q", n.Value)
		}
		return v, nil
	case "!!float":
		// yaml.v3 caps integer resolution at uint64 and silently degrades larger
		// integers to !!float with the original text intact — recover those as
		// *big.Int. Only do this for an *implicitly* resolved tag: `!!float 1`
		// written by hand is a float and must stay one.
		if n.Style&yamlv3.TaggedStyle == 0 && intLike.MatchString(n.Value) {
			// YAML 1.1 allows underscores as digit separators, and yaml.v3
			// resolves 1_000 as the integer 1000, so they must be stripped here
			// too or the separated spelling silently keeps the degraded float.
			if b, ok := new(big.Int).SetString(strings.ReplaceAll(n.Value, "_", ""), 10); ok {
				// Normalize back onto the int64 -> uint64 -> *big.Int ladder so a
				// recovered value carries the same type as a directly parsed one.
				return node.BigResult(b), nil
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
			return nil, format.New(format.YAML, n.Line, n.Column, "invalid float %q", n.Value)
		}
		return f, nil
	case "!!timestamp":
		// Keep the original text, never a time.Time.
		return n.Value, nil
	case "!!binary":
		return nil, format.New(format.YAML, n.Line, n.Column, "!!binary values are not supported")
	default:
		return nil, format.New(format.YAML, n.Line, n.Column, "unsupported tag %q", n.Tag)
	}
}

func wrapErr(err error) error {
	// yaml.v3 has its own depth cap and reaches it before our counter can fire;
	// map it onto the documented sentinel, as the JSON path does for the
	// standard library's equivalent message.
	if strings.Contains(err.Error(), "exceeded max depth") {
		return node.ErrTooDeep
	}
	var te *yamlv3.TypeError
	if errors.As(err, &te) && len(te.Errors) > 0 {
		return format.New(format.YAML, 0, 0, "%s", te.Errors[0])
	}
	return format.Wrap(format.YAML, 0, 0, err, err.Error())
}
