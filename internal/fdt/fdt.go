// Package fdt reads flattened device tree blobs (DTB, and the FIT image
// header, which uses the same format). It keeps the absolute offset of every
// property value so that a value can be patched in place without rebuilding
// the blob.
package fdt

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// Magic starts every device tree blob.
const Magic = 0xd00dfeed

const (
	tokenBeginNode = 1
	tokenEndNode   = 2
	tokenProp      = 3
	tokenNop       = 4
	tokenEnd       = 9

	headerSize = 40
	maxDepth   = 64
)

// ErrNotFDT means the data does not start with a device tree header.
var ErrNotFDT = errors.New("not a device tree blob")

// Tree is a parsed blob. Offsets in Prop are relative to the start of Data.
type Tree struct {
	Data []byte // the blob, TotalSize bytes
	Root *Node
}

// Node is one node of the tree.
type Node struct {
	Name     string // "" for the root, "charger", "pmic@20", ...
	Path     string // "/", "/i2c@fdd40000/pmic@20/charger"
	Props    []Prop
	Children []*Node
	Parent   *Node
}

// Prop is a property: its value is Data[Offset:Offset+Len].
type Prop struct {
	Name   string
	Offset int
	Len    int
}

// Header returns the total size a blob at data[0:] declares, after checking
// the magic. It lets callers find the end of a blob embedded in a larger
// buffer before parsing it.
func Header(data []byte) (totalSize int, err error) {
	if len(data) < headerSize || binary.BigEndian.Uint32(data) != Magic {
		return 0, ErrNotFDT
	}
	return int(binary.BigEndian.Uint32(data[4:])), nil
}

// Parse reads the blob at the start of data. data may be longer than the
// blob (a partition image); Tree.Data is cut to the declared total size.
func Parse(data []byte) (*Tree, error) {
	total, err := Header(data)
	if err != nil {
		return nil, err
	}
	if total < headerSize || total > len(data) {
		return nil, fmt.Errorf("fdt: total size %d outside the %d bytes available", total, len(data))
	}
	blob := data[:total:total]
	be := binary.BigEndian
	offStruct := int(be.Uint32(blob[8:]))
	offStrings := int(be.Uint32(blob[12:]))
	version := be.Uint32(blob[20:])
	if version < 16 {
		return nil, fmt.Errorf("fdt: unsupported version %d", version)
	}
	sizeStrings := int(be.Uint32(blob[32:]))
	sizeStruct := int(be.Uint32(blob[36:]))
	if !inBounds(offStruct, sizeStruct, total) || !inBounds(offStrings, sizeStrings, total) {
		return nil, errors.New("fdt: struct or strings block outside the blob")
	}
	strs := blob[offStrings : offStrings+sizeStrings]

	t := &Tree{Data: blob}
	p := offStruct
	end := offStruct + sizeStruct
	var cur *Node
	depth := 0
	for {
		if p+4 > end {
			return nil, errors.New("fdt: struct block ends without FDT_END")
		}
		tok := be.Uint32(blob[p:])
		p += 4
		switch tok {
		case tokenBeginNode:
			name, next, err := cString(blob, p, end)
			if err != nil {
				return nil, err
			}
			p = align4(next)
			if depth++; depth > maxDepth {
				return nil, errors.New("fdt: nodes nested too deeply")
			}
			n := &Node{Name: name, Parent: cur}
			switch {
			case cur == nil && t.Root != nil:
				return nil, errors.New("fdt: more than one root node")
			case cur == nil:
				n.Path = "/"
				t.Root = n
			default:
				n.Path = strings.TrimSuffix(cur.Path, "/") + "/" + name
				cur.Children = append(cur.Children, n)
			}
			cur = n
		case tokenEndNode:
			if cur == nil {
				return nil, errors.New("fdt: unbalanced end of node")
			}
			cur = cur.Parent
			depth--
		case tokenProp:
			if cur == nil {
				return nil, errors.New("fdt: property outside a node")
			}
			if p+8 > end {
				return nil, errors.New("fdt: truncated property")
			}
			ln := int(be.Uint32(blob[p:]))
			nameOff := int(be.Uint32(blob[p+4:]))
			p += 8
			if !inBounds(p, ln, end) {
				return nil, errors.New("fdt: property value outside the struct block")
			}
			name, _, err := cString(strs, nameOff, len(strs))
			if err != nil {
				return nil, fmt.Errorf("fdt: property name: %w", err)
			}
			cur.Props = append(cur.Props, Prop{Name: name, Offset: p, Len: ln})
			p = align4(p + ln)
		case tokenNop:
		case tokenEnd:
			if cur != nil || t.Root == nil {
				return nil, errors.New("fdt: FDT_END inside a node or before the root")
			}
			return t, nil
		default:
			return nil, fmt.Errorf("fdt: unknown token %#x at %#x", tok, p-4)
		}
	}
}

// Find returns the node at an absolute path ("/images/fdt"), or nil.
func (t *Tree) Find(path string) *Node {
	n := t.Root
	for _, part := range strings.Split(strings.Trim(path, "/"), "/") {
		if part == "" {
			continue
		}
		n = n.Child(part)
		if n == nil {
			return nil
		}
	}
	return n
}

// Walk visits every node depth first, stopping when fn returns false.
func (t *Tree) Walk(fn func(*Node) bool) {
	var walk func(*Node) bool
	walk = func(n *Node) bool {
		if !fn(n) {
			return false
		}
		for _, c := range n.Children {
			if !walk(c) {
				return false
			}
		}
		return true
	}
	walk(t.Root)
}

// Child returns the direct child with the given name, or nil.
func (n *Node) Child(name string) *Node {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// Prop returns the named property of the node.
func (n *Node) Prop(name string) (Prop, bool) {
	for _, p := range n.Props {
		if p.Name == name {
			return p, true
		}
	}
	return Prop{}, false
}

// Value returns the raw bytes of a property.
func (t *Tree) Value(p Prop) []byte { return t.Data[p.Offset : p.Offset+p.Len] }

// U32 reads a property that holds exactly one 32-bit cell.
func (t *Tree) U32(p Prop) (uint32, error) {
	if p.Len != 4 {
		return 0, fmt.Errorf("fdt: %s is %d bytes, not a single cell", p.Name, p.Len)
	}
	return binary.BigEndian.Uint32(t.Data[p.Offset:]), nil
}

// Strings reads a string-list property ("a\0b\0").
func (t *Tree) Strings(p Prop) []string {
	v := strings.TrimRight(string(t.Value(p)), "\x00")
	if v == "" {
		return nil
	}
	return strings.Split(v, "\x00")
}

// String reads the first string of a property.
func (t *Tree) String(p Prop) string {
	if s := t.Strings(p); len(s) > 0 {
		return s[0]
	}
	return ""
}

// PropU32 reads a single-cell property of a node by name.
func (t *Tree) PropU32(n *Node, name string) (uint32, Prop, error) {
	p, ok := n.Prop(name)
	if !ok {
		return 0, Prop{}, fmt.Errorf("fdt: %s has no %s", n.Path, name)
	}
	v, err := t.U32(p)
	return v, p, err
}

// PropString reads the first string of a node's property ("" if absent).
func (t *Tree) PropString(n *Node, name string) string {
	if p, ok := n.Prop(name); ok {
		return t.String(p)
	}
	return ""
}

// Compatible reports whether the node's compatible list holds value.
func (t *Tree) Compatible(n *Node, value string) bool {
	p, ok := n.Prop("compatible")
	if !ok {
		return false
	}
	for _, s := range t.Strings(p) {
		if s == value {
			return true
		}
	}
	return false
}

// FindCompatible returns the nodes whose compatible list holds value.
func (t *Tree) FindCompatible(value string) []*Node {
	var out []*Node
	t.Walk(func(n *Node) bool {
		if t.Compatible(n, value) {
			out = append(out, n)
		}
		return true
	})
	return out
}

func cString(b []byte, start, end int) (string, int, error) {
	if start < 0 || start >= end {
		return "", 0, errors.New("fdt: string outside its block")
	}
	for i := start; i < end; i++ {
		if b[i] == 0 {
			return string(b[start:i]), i + 1, nil
		}
	}
	return "", 0, errors.New("fdt: unterminated string")
}

func inBounds(off, size, limit int) bool {
	return off >= 0 && size >= 0 && off <= limit && size <= limit-off
}

func align4(n int) int { return (n + 3) &^ 3 }
