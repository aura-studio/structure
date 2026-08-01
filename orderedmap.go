package structure

import (
	"bytes"
	"iter"
)

// Node is the intermediate representation shared by Parse and Encode. A valid
// Node is exactly one of:
//
//   - *OrderedMap : mapping (string keys, insertion order)
//   - []Node      : array
//   - nil         : null / None / nil
//   - bool
//   - int64       : signed integer (preferred carrier)
//   - uint64      : unsigned integer beyond int64 range
//   - *big.Int    : arbitrary-precision integer beyond uint64 range
//   - float64     : floating point (NaN/±Inf only encodable to YAML and TOML)
//   - string      : text; time/date values normalize into strings
//
// Any other Go type is rejected by Encode. Documents must be rooted at a
// mapping or an array; a bare top-level scalar is rejected.
type Node = any

// pair is a doubly-linked list node backing OrderedMap.
type pair struct {
	key   string
	value Node
	prev  *pair
	next  *pair
}

// OrderedMap is an insertion-ordered string-keyed map with O(1) Get, Set and
// Delete. Iteration follows insertion order. Setting an existing key updates
// its value in place without moving it. It implements fmt-friendly iteration
// via All (iter.Seq2) and order-preserving JSON via MarshalJSON.
type OrderedMap struct {
	index map[string]*pair
	head  *pair
	tail  *pair
	len   int
}

// NewOrderedMap returns an empty OrderedMap ready for use.
func NewOrderedMap() *OrderedMap {
	return &OrderedMap{index: make(map[string]*pair)}
}

// Len returns the number of keys.
func (m *OrderedMap) Len() int { return m.len }

// Get returns the value stored under key.
func (m *OrderedMap) Get(key string) (Node, bool) {
	p, ok := m.index[key]
	if !ok {
		return nil, false
	}
	return p.value, true
}

// Set inserts key with value at the end of the order, or updates the value in
// place (position preserved) when key already exists.
func (m *OrderedMap) Set(key string, value Node) {
	if m.index == nil {
		m.index = make(map[string]*pair)
	}
	if p, ok := m.index[key]; ok {
		p.value = value
		return
	}
	p := &pair{key: key, value: value, prev: m.tail}
	if m.tail != nil {
		m.tail.next = p
	} else {
		m.head = p
	}
	m.tail = p
	m.index[key] = p
	m.len++
}

// Delete removes key, reporting whether it existed. The remaining order is
// preserved. O(1).
func (m *OrderedMap) Delete(key string) bool {
	p, ok := m.index[key]
	if !ok {
		return false
	}
	if p.prev != nil {
		p.prev.next = p.next
	} else {
		m.head = p.next
	}
	if p.next != nil {
		p.next.prev = p.prev
	} else {
		m.tail = p.prev
	}
	delete(m.index, key)
	m.len--
	return true
}

// Keys returns all keys in insertion order.
func (m *OrderedMap) Keys() []string {
	keys := make([]string, 0, m.len)
	for p := m.head; p != nil; p = p.next {
		keys = append(keys, p.key)
	}
	return keys
}

// All returns an iterator over key/value pairs in insertion order, suitable
// for range-over-func (Go 1.23+).
func (m *OrderedMap) All() iter.Seq2[string, Node] {
	return func(yield func(string, Node) bool) {
		for p := m.head; p != nil; p = p.next {
			if !yield(p.key, p.value) {
				return
			}
		}
	}
}

// Clone returns a deep copy: nested *OrderedMap and []Node values are cloned
// recursively; scalars (including *big.Int) are copied by value.
func (m *OrderedMap) Clone() *OrderedMap {
	c := NewOrderedMap()
	for p := m.head; p != nil; p = p.next {
		c.Set(p.key, nodeClone(p.value))
	}
	return c
}

// MarshalJSON renders the map as a JSON object preserving insertion order.
// It lets external encoding/json users get deterministic order when an
// OrderedMap appears inside marshaled data.
func (m *OrderedMap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true
	for p := m.head; p != nil; p = p.next {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.WriteString(jsonEscape(p.key))
		buf.WriteByte(':')
		b, err := jsonMarshalNode(p.value)
		if err != nil {
			return nil, err
		}
		buf.Write(b)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
