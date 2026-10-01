package fdt

import "encoding/binary"

// Builder writes a device tree blob. It exists for tests and for the
// development device (cmd/devseed): the app itself only patches blobs.
//
//	b := fdt.NewBuilder()
//	b.Begin("")
//	b.Begin("charger")
//	b.PropStrings("compatible", "rk817,charger")
//	b.PropU32("max_chrg_current", 2000)
//	b.End()
//	b.End()
//	blob := b.Bytes()
type Builder struct {
	strct   []byte
	strs    []byte
	nameOff map[string]int
}

// NewBuilder starts an empty blob.
func NewBuilder() *Builder { return &Builder{nameOff: map[string]int{}} }

// Begin opens a node; the first call opens the root (name "").
func (b *Builder) Begin(name string) {
	b.u32(tokenBeginNode)
	b.strct = append(b.strct, name...)
	b.strct = append(b.strct, 0)
	b.pad()
}

// End closes the innermost open node.
func (b *Builder) End() { b.u32(tokenEndNode) }

// Prop adds a property with a raw value.
func (b *Builder) Prop(name string, value []byte) {
	off, ok := b.nameOff[name]
	if !ok {
		off = len(b.strs)
		b.nameOff[name] = off
		b.strs = append(b.strs, name...)
		b.strs = append(b.strs, 0)
	}
	b.u32(tokenProp)
	b.u32(uint32(len(value)))
	b.u32(uint32(off))
	b.strct = append(b.strct, value...)
	b.pad()
}

// PropU32 adds a property of 32-bit cells.
func (b *Builder) PropU32(name string, cells ...uint32) {
	v := make([]byte, 4*len(cells))
	for i, c := range cells {
		binary.BigEndian.PutUint32(v[4*i:], c)
	}
	b.Prop(name, v)
}

// PropStrings adds a string-list property.
func (b *Builder) PropStrings(name string, values ...string) {
	var v []byte
	for _, s := range values {
		v = append(v, s...)
		v = append(v, 0)
	}
	b.Prop(name, v)
}

// Bytes finishes the blob: header, an empty memory reservation map, the
// struct block and the strings block.
func (b *Builder) Bytes() []byte {
	strct := append(append([]byte{}, b.strct...), 0, 0, 0, tokenEnd)
	const rsvOff = headerSize
	rsv := make([]byte, 16) // one terminating (0, 0) entry
	structOff := rsvOff + len(rsv)
	stringsOff := structOff + len(strct)
	total := stringsOff + len(b.strs)
	out := make([]byte, total)
	be := binary.BigEndian
	be.PutUint32(out[0:], Magic)
	be.PutUint32(out[4:], uint32(total))
	be.PutUint32(out[8:], uint32(structOff))
	be.PutUint32(out[12:], uint32(stringsOff))
	be.PutUint32(out[16:], uint32(rsvOff))
	be.PutUint32(out[20:], 17) // version
	be.PutUint32(out[24:], 16) // last compatible version
	be.PutUint32(out[32:], uint32(len(b.strs)))
	be.PutUint32(out[36:], uint32(len(strct)))
	copy(out[structOff:], strct)
	copy(out[stringsOff:], b.strs)
	return out
}

func (b *Builder) u32(v uint32) { b.strct = binary.BigEndian.AppendUint32(b.strct, v) }

func (b *Builder) pad() {
	for len(b.strct)%4 != 0 {
		b.strct = append(b.strct, 0)
	}
}
