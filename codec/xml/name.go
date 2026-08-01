package xml

import (
	"fmt"
	"strings"

	"github.com/aura-studio/structure/v2/node"
)

// nameStart reports whether r may begin an XML 1.0 Name (§2.3 NameStartChar).
func nameStart(r rune) bool {
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

// nameChar reports whether r may continue an XML 1.0 Name (§2.3 NameChar).
func nameChar(r rune) bool {
	switch {
	case nameStart(r),
		r == '-' || r == '.' || r == 0xB7,
		r >= '0' && r <= '9',
		r >= 0x300 && r <= 0x36F,
		r >= 0x203F && r <= 0x2040:
		return true
	}
	return false
}

// validName reports whether s is a well-formed XML 1.0 Name (§2.3). The encoder
// checks this because encoding/xml happily writes a malformed tag such as
// `<a b>` and reports no error, producing a document this package's own parser
// then rejects.
func validName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !nameStart(r) {
				return false
			}
			continue
		}
		if !nameChar(r) {
			return false
		}
	}
	return true
}

// validNCName reports whether s is a colon-free Name (an NCName in the XML
// Namespaces sense). Element names must be NCNames: a colon is a legal Name
// character, but the parser reads element names as Name.Local, so `<a:b>` comes
// back as the key "b" and the prefix would be lost silently.
func validNCName(s string) bool {
	return validName(s) && !strings.Contains(s, ":")
}

// validAttrName reports whether s is usable as an attribute name. Namespace
// declarations keep the colon form the parser emits for them ("xmlns:p"); every
// other name must be colon-free, because a prefixed attribute also comes back
// as its Local part alone.
func validAttrName(s string) bool {
	if validNCName(s) {
		return true
	}
	local, found := strings.CutPrefix(s, "xmlns:")
	return found && validNCName(local)
}

// checkChars rejects text that XML 1.0 §2.2 Char cannot represent. Only #x9,
// #xA and #xD are legal below #x20; encoding/xml would otherwise silently
// substitute U+FFFD and corrupt the value.
func checkChars(s string) error {
	for _, r := range s {
		switch {
		case r == 0x9 || r == 0xA || r == 0xD,
			r >= 0x20 && r <= 0xD7FF,
			r >= 0xE000 && r <= 0xFFFD,
			r >= 0x10000 && r <= 0x10FFFF:
		default:
			return fmt.Errorf("%w: XML cannot represent character U+%04X", node.ErrUnsupportedStructure, r)
		}
	}
	return nil
}
