package structure

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// XML mapping conventions (Requirement 4): attributes become "@name" keys,
// element text becomes "#text", same-name siblings merge into arrays, values
// stay strings (XML is typeless), single root element enforced.

// parseXML decodes an XML document into {rootName: content}.
func parseXML(input string) (Node, error) {
	dec := xml.NewDecoder(strings.NewReader(input))

	type frame struct {
		name string
		m    *OrderedMap
	}
	var stack []frame
	var root *OrderedMap
	var rootName string
	depth := 0

	xmlErr := func(msg string) error {
		line, col := xmlLineCol(input, dec.InputOffset())
		return newParseError(XML, line, col, "%s", msg)
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			line, col := xmlLineCol(input, dec.InputOffset())
			return nil, &ParseError{Format: XML, Line: line, Column: col, Msg: err.Error(), err: err}
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if err := tooDeep(depth); err != nil {
				return nil, err
			}
			f := frame{name: t.Name.Local, m: NewOrderedMap()}
			for _, a := range t.Attr {
				f.m.Set("@"+xmlAttrKey(a.Name), a.Value)
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, xmlErr("XML requires exactly one root element")
				}
				root, rootName = f.m, f.name
			}
			stack = append(stack, f)
		case xml.EndElement:
			depth--
			if len(stack) == 0 {
				return nil, xmlErr("unbalanced end tag")
			}
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				continue // root closed
			}
			parent := &stack[len(stack)-1]
			xmlAddChild(parent.m, top.name, xmlElementValue(top.m))
		case xml.CharData:
			text := string(t)
			if len(stack) == 0 {
				if strings.TrimSpace(text) != "" {
					return nil, xmlErr("text outside the root element")
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
		case xml.Comment, xml.ProcInst, xml.Directive:
			// Ignored by convention (Requirement 4.2).
		}
	}
	if root == nil {
		return nil, newParseError(XML, 0, 0, "no root element")
	}
	result := NewOrderedMap()
	result.Set(rootName, xmlElementValue(root))
	return result, nil
}

// xmlAttrKey maps an attribute name to its key suffix: xmlns declarations
// keep their form ("xmlns", "xmlns:p"); everything else uses the Local name.
func xmlAttrKey(n xml.Name) string {
	if n.Space == "xmlns" {
		return "xmlns:" + n.Local
	}
	return n.Local
}

// xmlElementValue flattens an element's map: a lone "#text" key becomes a
// plain string value, and an element with no attributes, text or children
// becomes the empty string (design: an empty element carries an absent #text,
// i.e. "", which is what the encoder emits for "" and for nil).
func xmlElementValue(m *OrderedMap) Node {
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

// xmlAddChild inserts a child element under name, merging same-name siblings
// into an array on the second occurrence.
func xmlAddChild(parent *OrderedMap, name string, value Node) {
	prev, ok := parent.Get(name)
	if !ok {
		parent.Set(name, value)
		return
	}
	if arr, isArr := prev.([]Node); isArr {
		parent.Set(name, append(arr, value))
		return
	}
	parent.Set(name, []Node{prev, value})
}

// xmlLineCol converts a byte offset into 1-based line/col.
func xmlLineCol(input string, offset int64) (int, int) {
	if offset < 0 {
		return 0, 0
	}
	if int(offset) > len(input) {
		offset = int64(len(input))
	}
	line, col := 1, 1
	for i := int64(0); i < offset; i++ {
		if input[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

// encodeXML renders a Node tree as indented XML with a leading declaration.
func encodeXML(n Node) (string, error) {
	if err := validate(n); err != nil {
		return "", err
	}
	m, ok := n.(*OrderedMap)
	if !ok {
		return "", fmt.Errorf("%w: XML requires a single root element (got array)", ErrUnsupportedStructure)
	}
	if m.Len() != 1 {
		return "", fmt.Errorf("%w: XML requires exactly one root element (got %d top-level keys)", ErrUnsupportedStructure, m.Len())
	}
	var rootName string
	var content Node
	for k, v := range m.All() {
		rootName, content = k, v
	}
	if _, isArr := content.([]Node); isArr {
		return "", fmt.Errorf("%w: XML root content cannot be an array", ErrUnsupportedStructure)
	}

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	// Indentation is whitespace *inside* an element, so for mixed content (an
	// element holding both #text and child elements) it becomes part of the text
	// on the way back in. Round-trip fidelity outranks pretty output, so such a
	// document is written unindented.
	if !xmlHasMixedContent(content) {
		enc.Indent("", "  ")
	}
	if err := writeXMLElement(enc, rootName, content); err != nil {
		return "", err
	}
	if err := enc.Flush(); err != nil {
		return "", fmt.Errorf("xml: encode: %w", err)
	}
	buf.WriteByte('\n')
	return buf.String(), nil
}

// writeXMLElement emits one element (with attributes/text/children) for v.
func writeXMLElement(enc *xml.Encoder, name string, v Node) error {
	if !xmlValidNCName(name) {
		return fmt.Errorf("%w: %q is not a valid XML element name", ErrUnsupportedStructure, name)
	}
	start := xml.StartElement{Name: xml.Name{Local: name}}

	switch tv := v.(type) {
	case *OrderedMap:
		for k, av := range tv.All() {
			if !strings.HasPrefix(k, "@") {
				continue
			}
			attrName := strings.TrimPrefix(k, "@")
			// An xmlns declaration keeps its colon form; both halves must still
			// be valid names.
			if !xmlValidAttrName(attrName) {
				return fmt.Errorf("%w: %q is not a valid XML attribute name", ErrUnsupportedStructure, k)
			}
			s, err := xmlScalarString(av)
			if err != nil {
				return fmt.Errorf("xml: attribute %q: %w", k, err)
			}
			start.Attr = append(start.Attr, xml.Attr{
				Name:  xml.Name{Local: attrName},
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
				s, err := xmlScalarString(cv)
				if err != nil {
					return fmt.Errorf("xml: #text: %w", err)
				}
				if err := enc.EncodeToken(xml.CharData(s)); err != nil {
					return err
				}
				continue
			}
			if arr, isArr := cv.([]Node); isArr {
				for _, e := range arr {
					if err := writeXMLElement(enc, k, e); err != nil {
						return err
					}
				}
				continue
			}
			if err := writeXMLElement(enc, k, cv); err != nil {
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
		s, err := xmlScalarString(v)
		if err != nil {
			return err
		}
		if err := enc.EncodeToken(start); err != nil {
			return err
		}
		if err := enc.EncodeToken(xml.CharData(s)); err != nil {
			return err
		}
		return enc.EncodeToken(start.End())
	}
}

// xmlHasMixedContent reports whether any element in the tree carries both text
// and child elements. Encoding such a tree with indentation injects whitespace
// that the parser then folds into #text, so the value would not round-trip.
func xmlHasMixedContent(n Node) bool {
	switch v := n.(type) {
	case *OrderedMap:
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
			if xmlHasMixedContent(cv) {
				return true
			}
		}
	case []Node:
		for _, e := range v {
			if xmlHasMixedContent(e) {
				return true
			}
		}
	}
	return false
}

// xmlScalarString stringifies a scalar for XML text/attribute position.
// NaN/Inf are rejected (no XML representation).
func xmlScalarString(v Node) (string, error) {
	switch tv := v.(type) {
	case string:
		return tv, xmlCheckChars(tv)
	case nil:
		return "", nil
	case bool:
		return strconv.FormatBool(tv), nil
	}
	if isNumeric(v) {
		return formatNumber(v)
	}
	return "", fmt.Errorf("%w: XML values must be scalars (got %T)", ErrUnsupportedStructure, v)
}

// xmlNameStart reports whether r may begin an XML 1.0 Name (§2.3 NameStartChar).
func xmlNameStart(r rune) bool {
	switch {
	case r == ':' || r == '_',
		r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z',
		r >= 0xC0 && r <= 0xD6, r >= 0xD8 && r <= 0xF6, r >= 0xF8 && r <= 0x2FF,
		r >= 0x370 && r <= 0x37D, r >= 0x37F && r <= 0x1FFF,
		r >= 0x200C && r <= 0x200D, r >= 0x2070 && r <= 0x218F,
		r >= 0x2C00 && r <= 0x2FEF, r >= 0x3001 && r <= 0xD7FF,
		r >= 0xF900 && r <= 0xFDCF, r >= 0xFDF0 && r <= 0xFFFD,
		r >= 0x10000 && r <= 0xEFFFF:
		return true
	}
	return false
}

// xmlNameChar reports whether r may continue an XML 1.0 Name (§2.3 NameChar).
func xmlNameChar(r rune) bool {
	switch {
	case xmlNameStart(r),
		r == '-' || r == '.' || r == 0xB7,
		r >= '0' && r <= '9',
		r >= 0x300 && r <= 0x36F,
		r >= 0x203F && r <= 0x2040:
		return true
	}
	return false
}

// xmlValidName reports whether s is a well-formed XML 1.0 Name (§2.3). The
// encoder checks this because encoding/xml happily writes a malformed tag such
// as `<a b>` and reports no error, producing a document this package's own
// parser then rejects.
func xmlValidName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !xmlNameStart(r) {
				return false
			}
			continue
		}
		if !xmlNameChar(r) {
			return false
		}
	}
	return true
}

// xmlValidNCName reports whether s is a colon-free Name (an NCName in the XML
// Namespaces sense). Element names must be NCNames: a colon is a legal Name
// character, but the parser reads element names as Name.Local, so `<a:b>` comes
// back as the key "b" and the prefix would be lost silently.
func xmlValidNCName(s string) bool {
	return xmlValidName(s) && !strings.Contains(s, ":")
}

// xmlValidAttrName reports whether s is usable as an attribute name. Namespace
// declarations keep the colon form the parser emits for them ("xmlns:p"); every
// other name must be colon-free, because a prefixed attribute also comes back
// as its Local part alone.
func xmlValidAttrName(s string) bool {
	if xmlValidNCName(s) {
		return true
	}
	local, found := strings.CutPrefix(s, "xmlns:")
	return found && xmlValidNCName(local)
}

// xmlCheckChars rejects text that XML 1.0 §2.2 Char cannot represent. Only
// #x9, #xA and #xD are legal below #x20; encoding/xml would otherwise silently
// substitute U+FFFD and corrupt the value.
func xmlCheckChars(s string) error {
	for _, r := range s {
		switch {
		case r == 0x9 || r == 0xA || r == 0xD,
			r >= 0x20 && r <= 0xD7FF,
			r >= 0xE000 && r <= 0xFFFD,
			r >= 0x10000 && r <= 0x10FFFF:
		default:
			return fmt.Errorf("%w: XML cannot represent character U+%04X", ErrUnsupportedStructure, r)
		}
	}
	return nil
}
