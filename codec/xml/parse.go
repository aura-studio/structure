// Package xml parses and encodes XML text as a node.Node tree.
//
// Mapping conventions: attributes become "@name" keys, element text becomes
// "#text", same-name siblings merge into arrays, values stay strings (XML is
// typeless), and a document must have exactly one root element.
package xml

import (
	stdxml "encoding/xml"
	"io"
	"strings"

	"github.com/aura-studio/structure/v2/format"
	"github.com/aura-studio/structure/v2/node"
)

// Parse decodes an XML document into {rootName: content}.
func Parse(input string) (node.Node, error) {
	dec := stdxml.NewDecoder(strings.NewReader(input))

	type frame struct {
		name string
		m    *node.OrderedMap
	}
	var stack []frame
	var root *node.OrderedMap
	var rootName string
	depth := 0

	posErr := func(msg string) error {
		line, col := format.LineCol(input, dec.InputOffset())
		return format.New(format.XML, line, col, "%s", msg)
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			line, col := format.LineCol(input, dec.InputOffset())
			return nil, format.Wrap(format.XML, line, col, err, err.Error())
		}
		switch t := tok.(type) {
		case stdxml.StartElement:
			depth++
			if err := node.TooDeep(depth); err != nil {
				return nil, err
			}
			f := frame{name: t.Name.Local, m: node.NewOrderedMap()}
			for _, a := range t.Attr {
				f.m.Set("@"+attrKey(a.Name), a.Value)
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, posErr("XML requires exactly one root element")
				}
				root, rootName = f.m, f.name
			}
			stack = append(stack, f)
		case stdxml.EndElement:
			depth--
			if len(stack) == 0 {
				return nil, posErr("unbalanced end tag")
			}
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				continue // root closed
			}
			parent := &stack[len(stack)-1]
			addChild(parent.m, top.name, elementValue(top.m))
		case stdxml.CharData:
			text := string(t)
			if len(stack) == 0 {
				if strings.TrimSpace(text) != "" {
					return nil, posErr("text outside the root element")
				}
				continue
			}
			// Token() does not skip whitespace-only text; drop it here.
			if strings.TrimSpace(text) == "" {
				continue
			}
			top := &stack[len(stack)-1]
			if prev, ok := top.m.Get("#text"); ok {
				top.m.Set("#text", prev.(string)+text)
			} else {
				top.m.Set("#text", text)
			}
		case stdxml.Comment, stdxml.ProcInst, stdxml.Directive:
			// Ignored by convention.
		}
	}
	if root == nil {
		return nil, format.New(format.XML, 0, 0, "no root element")
	}
	result := node.NewOrderedMap()
	result.Set(rootName, elementValue(root))
	return result, nil
}

// attrKey maps an attribute name to its key suffix: xmlns declarations keep
// their form ("xmlns", "xmlns:p"); everything else uses the Local name.
func attrKey(n stdxml.Name) string {
	if n.Space == "xmlns" {
		return "xmlns:" + n.Local
	}
	return n.Local
}

// elementValue flattens an element's map: a lone "#text" key becomes a plain
// string value, and an element with no attributes, text or children becomes the
// empty string (design: an empty element carries an absent #text, i.e. "",
// which is what the encoder emits for "" and for nil).
func elementValue(m *node.OrderedMap) node.Node {
	switch m.Len() {
	case 0:
		return ""
	case 1:
		if v, ok := m.Get("#text"); ok {
			return v
		}
	}
	return m
}

// addChild inserts a child element under name, merging same-name siblings into
// an array on the second occurrence.
func addChild(parent *node.OrderedMap, name string, value node.Node) {
	prev, ok := parent.Get(name)
	if !ok {
		parent.Set(name, value)
		return
	}
	if arr, isArr := prev.([]node.Node); isArr {
		parent.Set(name, append(arr, value))
		return
	}
	parent.Set(name, []node.Node{prev, value})
}
