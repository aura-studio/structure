package xml

import (
	"bytes"
	stdxml "encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"github.com/aura-studio/structure/v2/node"
)

// Encode renders a node.Node tree as indented XML with a leading declaration.
func Encode(n node.Node) (string, error) {
	if err := node.Validate(n); err != nil {
		return "", err
	}
	m, ok := n.(*node.OrderedMap)
	if !ok {
		return "", fmt.Errorf("%w: XML requires a single root element (got array)", node.ErrUnsupportedStructure)
	}
	if m.Len() != 1 {
		return "", fmt.Errorf("%w: XML requires exactly one root element (got %d top-level keys)", node.ErrUnsupportedStructure, m.Len())
	}
	var rootName string
	var content node.Node
	for k, v := range m.All() {
		rootName, content = k, v
	}
	if _, isArr := content.([]node.Node); isArr {
		return "", fmt.Errorf("%w: XML root content cannot be an array", node.ErrUnsupportedStructure)
	}

	var buf bytes.Buffer
	buf.WriteString(stdxml.Header)
	enc := stdxml.NewEncoder(&buf)
	// Indentation is whitespace *inside* an element, so for mixed content (an
	// element holding both #text and child elements) it becomes part of the text
	// on the way back in. Round-trip fidelity outranks pretty output, so such a
	// document is written unindented.
	if !hasMixedContent(content) {
		enc.Indent("", "  ")
	}
	if err := writeElement(enc, rootName, content); err != nil {
		return "", err
	}
	if err := enc.Flush(); err != nil {
		return "", fmt.Errorf("xml: encode: %w", err)
	}
	buf.WriteByte('\n')
	return buf.String(), nil
}

// writeElement emits one element (with attributes/text/children) for v.
func writeElement(enc *stdxml.Encoder, name string, v node.Node) error {
	if !validNCName(name) {
		return fmt.Errorf("%w: %q is not a valid XML element name", node.ErrUnsupportedStructure, name)
	}
	start := stdxml.StartElement{Name: stdxml.Name{Local: name}}

	switch tv := v.(type) {
	case *node.OrderedMap:
		for k, av := range tv.All() {
			if !strings.HasPrefix(k, "@") {
				continue
			}
			attrName := strings.TrimPrefix(k, "@")
			// An xmlns declaration keeps its colon form; both halves must still
			// be valid names.
			if !validAttrName(attrName) {
				return fmt.Errorf("%w: %q is not a valid XML attribute name", node.ErrUnsupportedStructure, k)
			}
			s, err := scalarString(av)
			if err != nil {
				return fmt.Errorf("xml: attribute %q: %w", k, err)
			}
			start.Attr = append(start.Attr, stdxml.Attr{
				Name:  stdxml.Name{Local: attrName},
				Value: s,
			})
		}
		if err := enc.EncodeToken(start); err != nil {
			return err
		}
		for k, cv := range tv.All() {
			if strings.HasPrefix(k, "@") {
				continue
			}
			if k == "#text" {
				s, err := scalarString(cv)
				if err != nil {
					return fmt.Errorf("xml: #text: %w", err)
				}
				if err := enc.EncodeToken(stdxml.CharData(s)); err != nil {
					return err
				}
				continue
			}
			if arr, isArr := cv.([]node.Node); isArr {
				for _, e := range arr {
					if err := writeElement(enc, k, e); err != nil {
						return err
					}
				}
				continue
			}
			if err := writeElement(enc, k, cv); err != nil {
				return err
			}
		}
		return enc.EncodeToken(start.End())

	case nil:
		if err := enc.EncodeToken(start); err != nil {
			return err
		}
		return enc.EncodeToken(start.End())

	default:
		s, err := scalarString(v)
		if err != nil {
			return err
		}
		if err := enc.EncodeToken(start); err != nil {
			return err
		}
		if err := enc.EncodeToken(stdxml.CharData(s)); err != nil {
			return err
		}
		return enc.EncodeToken(start.End())
	}
}

// hasMixedContent reports whether any element in the tree carries both text and
// child elements. Encoding such a tree with indentation injects whitespace that
// the parser then folds into #text, so the value would not round-trip.
func hasMixedContent(n node.Node) bool {
	switch v := n.(type) {
	case *node.OrderedMap:
		_, hasText := v.Get("#text")
		for k, cv := range v.All() {
			if strings.HasPrefix(k, "@") {
				continue
			}
			if k == "#text" {
				continue
			}
			if hasText {
				return true // text plus an element child
			}
			if hasMixedContent(cv) {
				return true
			}
		}
	case []node.Node:
		for _, e := range v {
			if hasMixedContent(e) {
				return true
			}
		}
	}
	return false
}

// scalarString stringifies a scalar for XML text/attribute position. NaN/Inf
// are rejected (no XML representation).
func scalarString(v node.Node) (string, error) {
	switch tv := v.(type) {
	case string:
		return tv, checkChars(tv)
	case nil:
		return "", nil
	case bool:
		return strconv.FormatBool(tv), nil
	}
	if node.IsNumeric(v) {
		return node.FormatNumber(v)
	}
	return "", fmt.Errorf("%w: XML values must be scalars (got %T)", node.ErrUnsupportedStructure, v)
}
