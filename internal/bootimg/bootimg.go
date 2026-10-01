// Package bootimg reads and patches the RG DS Plus boot partition: a U-Boot
// FIT image with external data whose parts are the kernel device tree
// ("fdt"), the kernel and a Rockchip resource image ("resource", RSCE) that
// holds a second copy of the device tree as rk-kernel.dtb.
//
// U-Boot checks the sha256 of every FIT part and the SHA-1 of rk-kernel.dtb
// in the resource image, but no signature. Patch changes the charger's two
// current limits in both device tree copies and recomputes exactly those
// hashes; everything else stays byte for byte.
package bootimg

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"strings"

	"rgdsplus-charge-tune/internal/fdt"
)

// Device tree names of the RK817 charger (Rockchip BSP driver rk817_charger).
const (
	ChargerCompatible = "rk817,charger"
	PropChargeCurrent = "max_chrg_current"  // mA
	PropInputCurrent  = "max_input_current" // mA
	PropChargeVoltage = "max_chrg_voltage"  // mV, never changed
	ResourceDTBName   = "rk-kernel.dtb"
)

var (
	// ErrNotFIT means the data is not a FIT image with an /images node.
	ErrNotFIT = errors.New("not a FIT boot image")
	// ErrSigned means the image carries a signature, which a patch would
	// break; this image cannot be changed.
	ErrSigned = errors.New("the boot image is signed")
	// ErrNoCharger means no device tree in the image has the charger node.
	ErrNoCharger = errors.New("no " + ChargerCompatible + " node in the boot image")
	// ErrCopiesDiffer means the device tree copies disagree on the charger
	// values, so the image was not made the way this package expects.
	ErrCopiesDiffer = errors.New("the device tree copies in the boot image differ")
	// ErrMissingCopy means the image does not hold exactly one device tree
	// in the fdt part and one in the resource image. U-Boot may load either,
	// so a patch of only one could be written and never take effect.
	ErrMissingCopy = errors.New("the boot image does not hold one device tree in fdt and one in resource/" + ResourceDTBName)
)

// UnsupportedError is a structure this package does not know how to keep
// consistent after a patch (a hash algorithm, a layout).
type UnsupportedError struct{ What string }

func (e *UnsupportedError) Error() string { return "unsupported boot image: " + e.What }

// HashMismatchError means stored and computed hashes differ: the image is
// damaged or was changed without updating its hashes.
type HashMismatchError struct{ Part string }

func (e *HashMismatchError) Error() string { return "hash mismatch in " + e.Part }

// Part is an image under /images of the FIT header.
type Part struct {
	Name     string // "fdt", "kernel", "resource"
	Type     string // "flat_dt", "kernel", "multi"
	Offset   int    // absolute offset of the data
	Size     int
	HashAlgo string // "sha256", "sha1" or "" (no hash node)
	HashOff  int    // absolute offset of the stored hash value
	HashLen  int
}

// Charger is the charger node of one device tree copy. The offsets are
// absolute offsets of the 4-byte big-endian values in the image.
type Charger struct {
	Path          string
	ChargeCurrent uint32
	InputCurrent  uint32
	ChargeVoltage uint32
	ChargeOff     int
	InputOff      int
	VoltageOff    int
}

// DTBCopy is one copy of the kernel device tree inside the image.
type DTBCopy struct {
	Where      string // "fdt" or "resource/rk-kernel.dtb"
	InResource bool   // rk-kernel.dtb of the resource image (else a flat_dt part)
	Part       *Part  // the FIT part that contains it
	Offset     int    // absolute offset of the blob
	Size       int
	Compatible []string // root compatible list
	Charger    Charger
	// The resource entry's own hash of the blob (0 length: none).
	EntryHashAlgo string
	EntryHashOff  int
	EntryHashLen  int
}

// Image is a parsed boot image. Data is the whole partition; it is not
// copied, so callers must not change it while the Image is in use.
type Image struct {
	Data   []byte
	Parts  []*Part
	Copies []*DTBCopy
	Signed bool
}

// Values are the charger settings of an image.
type Values struct {
	ChargeCurrent uint32 // mA
	InputCurrent  uint32 // mA
	ChargeVoltage uint32 // mV
}

// Parse reads the FIT header and finds the device tree copies. It does not
// check hashes (Verify does), so that a damaged image can still be shown.
func Parse(data []byte) (*Image, error) {
	fit, err := fdt.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotFIT, err)
	}
	images := fit.Find("/images")
	if images == nil || len(images.Children) == 0 {
		return nil, ErrNotFIT
	}
	img := &Image{Data: data}
	fit.Walk(func(n *fdt.Node) bool {
		if strings.HasPrefix(n.Name, "signature") {
			if _, ok := n.Prop("value"); ok {
				img.Signed = true
			}
		}
		return true
	})
	for _, n := range images.Children {
		p, err := parsePart(fit, n, len(data))
		if err != nil {
			return nil, err
		}
		img.Parts = append(img.Parts, p)
	}
	for _, p := range img.Parts {
		switch {
		case p.Type == "flat_dt":
			c, err := parseDTB(data, p, p.Offset, p.Size, p.Name)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", p.Name, err)
			}
			img.Copies = append(img.Copies, c)
		case p.Name == "resource" || p.Type == "multi":
			c, err := parseResource(data, p)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", p.Name, err)
			}
			if c != nil {
				img.Copies = append(img.Copies, c)
			}
		}
	}
	if len(img.Copies) == 0 {
		return nil, ErrNoCharger
	}
	fdts, resources := 0, 0
	for _, c := range img.Copies {
		if c.InResource {
			resources++
		} else {
			fdts++
		}
	}
	if fdts != 1 || resources != 1 {
		return nil, ErrMissingCopy
	}
	first := img.Copies[0].Charger
	for _, c := range img.Copies[1:] {
		if c.Charger.ChargeCurrent != first.ChargeCurrent || c.Charger.InputCurrent != first.InputCurrent ||
			c.Charger.ChargeVoltage != first.ChargeVoltage {
			return nil, ErrCopiesDiffer
		}
	}
	return img, nil
}

func parsePart(fit *fdt.Tree, n *fdt.Node, dataLen int) (*Part, error) {
	p := &Part{Name: n.Name, Type: fit.PropString(n, "type")}
	if pos, ok := n.Prop("data-position"); ok {
		v, err := fit.U32(pos)
		if err != nil {
			return nil, err
		}
		p.Offset = int(v)
	} else if off, ok := n.Prop("data-offset"); ok {
		v, err := fit.U32(off)
		if err != nil {
			return nil, err
		}
		p.Offset = (len(fit.Data)+3)&^3 + int(v)
	} else if inline, ok := n.Prop("data"); ok {
		p.Offset, p.Size = inline.Offset, inline.Len
	} else {
		return nil, &UnsupportedError{What: n.Name + " has no data"}
	}
	if sz, ok := n.Prop("data-size"); ok {
		v, err := fit.U32(sz)
		if err != nil {
			return nil, err
		}
		p.Size = int(v)
	}
	if p.Offset < 0 || p.Size < 0 || p.Offset > dataLen || p.Size > dataLen-p.Offset {
		return nil, fmt.Errorf("%s: data %#x+%#x outside the %d-byte image", n.Name, p.Offset, p.Size, dataLen)
	}
	for _, h := range n.Children {
		if h.Name != "hash" && !strings.HasPrefix(h.Name, "hash-") && !strings.HasPrefix(h.Name, "hash@") {
			continue
		}
		if p.HashAlgo != "" {
			return nil, &UnsupportedError{What: n.Name + " has more than one hash"}
		}
		algo := fit.PropString(h, "algo")
		want := hashLen(algo)
		if want == 0 {
			return nil, &UnsupportedError{What: n.Name + " hash algorithm " + algo}
		}
		v, ok := h.Prop("value")
		if !ok || v.Len != want {
			return nil, &UnsupportedError{What: n.Name + " hash value"}
		}
		p.HashAlgo, p.HashOff, p.HashLen = algo, v.Offset, v.Len
	}
	return p, nil
}

// parseDTB reads a device tree copy and its charger node.
func parseDTB(data []byte, part *Part, off, size int, where string) (*DTBCopy, error) {
	total, err := fdt.Header(data[off:])
	if err != nil {
		return nil, err
	}
	if total > size {
		return nil, fmt.Errorf("device tree of %d bytes in a %d-byte part", total, size)
	}
	tree, err := fdt.Parse(data[off : off+size])
	if err != nil {
		return nil, err
	}
	nodes := tree.FindCompatible(ChargerCompatible)
	if len(nodes) != 1 {
		return nil, fmt.Errorf("%w (%d found)", ErrNoCharger, len(nodes))
	}
	n := nodes[0]
	c := &DTBCopy{Where: where, Part: part, Offset: off, Size: total}
	if p, ok := tree.Root.Prop("compatible"); ok {
		c.Compatible = tree.Strings(p)
	}
	ch := Charger{Path: n.Path}
	for _, f := range []struct {
		name string
		val  *uint32
		off  *int
	}{
		{PropChargeCurrent, &ch.ChargeCurrent, &ch.ChargeOff},
		{PropInputCurrent, &ch.InputCurrent, &ch.InputOff},
		{PropChargeVoltage, &ch.ChargeVoltage, &ch.VoltageOff},
	} {
		v, p, err := tree.PropU32(n, f.name)
		if err != nil {
			return nil, err
		}
		*f.val, *f.off = v, off+p.Offset
	}
	c.Charger = ch
	return c, nil
}

// Rockchip resource image (resource_tool): a header block, a table of
// entries, then the files, all addressed in 512-byte blocks. An entry is
// tag "ENTR", name[220], hash[32], hash_size, block offset, byte size (u32
// little-endian each).
const (
	rsceBlock       = 512
	rsceNameLen     = 220
	rsceHashOff     = 4 + rsceNameLen
	rsceHashSizeOff = rsceHashOff + 32
	rsceBlkOff      = rsceHashSizeOff + 4
	rsceSizeOff     = rsceBlkOff + 4
	rsceEntryMin    = rsceSizeOff + 4
)

// parseResource finds rk-kernel.dtb in a resource image. A resource image
// without it has no device tree copy (nil, nil).
func parseResource(data []byte, part *Part) (*DTBCopy, error) {
	r := data[part.Offset : part.Offset+part.Size]
	if len(r) < 16 || string(r[:4]) != "RSCE" {
		return nil, &UnsupportedError{What: part.Name + " is not a Rockchip resource image"}
	}
	tblOff := int(r[9]) * rsceBlock
	entryBlocks := int(r[10])
	count := int(binary.LittleEndian.Uint32(r[12:]))
	if entryBlocks < 1 || count < 0 || count > 1024 {
		return nil, &UnsupportedError{What: part.Name + " entry table"}
	}
	var found *DTBCopy
	for i := 0; i < count; i++ {
		e := tblOff + i*entryBlocks*rsceBlock
		if e < 0 || e+rsceEntryMin > len(r) {
			return nil, fmt.Errorf("resource entry %d outside the image", i)
		}
		if string(r[e:e+4]) != "ENTR" {
			return nil, &UnsupportedError{What: fmt.Sprintf("resource entry %d tag", i)}
		}
		name := string(bytes.TrimRight(r[e+4:e+4+rsceNameLen], "\x00"))
		if name != ResourceDTBName {
			continue
		}
		if found != nil {
			return nil, &UnsupportedError{What: "two " + ResourceDTBName + " entries"}
		}
		hashSize := int(binary.LittleEndian.Uint32(r[e+rsceHashSizeOff:]))
		blk := int(binary.LittleEndian.Uint32(r[e+rsceBlkOff:]))
		size := int(binary.LittleEndian.Uint32(r[e+rsceSizeOff:]))
		off := blk * rsceBlock
		if off < 0 || size <= 0 || off > len(r) || size > len(r)-off {
			return nil, fmt.Errorf("%s outside the resource image", ResourceDTBName)
		}
		c, err := parseDTB(data, part, part.Offset+off, size, part.Name+"/"+ResourceDTBName)
		if err != nil {
			return nil, err
		}
		if c.Size != size {
			return nil, &UnsupportedError{What: ResourceDTBName + " size differs from its device tree"}
		}
		c.InResource = true
		switch hashSize {
		case 0:
		case sha1.Size:
			c.EntryHashAlgo = "sha1"
		case sha256.Size:
			c.EntryHashAlgo = "sha256"
		default:
			return nil, &UnsupportedError{What: fmt.Sprintf("%s hash of %d bytes", ResourceDTBName, hashSize)}
		}
		if hashSize > 0 {
			c.EntryHashOff = part.Offset + e + rsceHashOff
			c.EntryHashLen = hashSize
		}
		found = c
	}
	return found, nil
}

func hashLen(algo string) int {
	switch algo {
	case "sha256":
		return sha256.Size
	case "sha1":
		return sha1.Size
	}
	return 0
}

func newHash(algo string) hash.Hash {
	if algo == "sha1" {
		return sha1.New()
	}
	return sha256.New()
}

func sum(algo string, b []byte) []byte {
	h := newHash(algo)
	h.Write(b)
	return h.Sum(nil)
}

// Values returns the charger settings (all copies agree, see Parse).
func (img *Image) Values() Values {
	c := img.Copies[0].Charger
	return Values{ChargeCurrent: c.ChargeCurrent, InputCurrent: c.InputCurrent, ChargeVoltage: c.ChargeVoltage}
}

// Verify checks every stored hash against the data.
func (img *Image) Verify() error {
	for _, c := range img.Copies {
		if c.EntryHashLen > 0 &&
			!bytes.Equal(sum(c.EntryHashAlgo, img.Data[c.Offset:c.Offset+c.Size]), img.Data[c.EntryHashOff:c.EntryHashOff+c.EntryHashLen]) {
			return &HashMismatchError{Part: c.Where}
		}
	}
	for _, p := range img.Parts {
		if p.HashLen > 0 &&
			!bytes.Equal(sum(p.HashAlgo, img.Data[p.Offset:p.Offset+p.Size]), img.Data[p.HashOff:p.HashOff+p.HashLen]) {
			return &HashMismatchError{Part: p.Name}
		}
	}
	return nil
}

// Range is a span of bytes [Off, Off+Len).
type Range struct{ Off, Len int }

// PatchRanges are the bytes a patch may change: the two current limits in
// every device tree copy, the resource entry hashes and the hashes of the
// FIT parts holding a copy. The charge voltage is not among them.
func (img *Image) PatchRanges() []Range {
	var out []Range
	holders := map[*Part]bool{}
	for _, c := range img.Copies {
		out = append(out, Range{c.Charger.ChargeOff, 4}, Range{c.Charger.InputOff, 4})
		if c.EntryHashLen > 0 {
			out = append(out, Range{c.EntryHashOff, c.EntryHashLen})
		}
		holders[c.Part] = true
	}
	for _, p := range img.Parts {
		if holders[p] && p.HashLen > 0 {
			out = append(out, Range{p.HashOff, p.HashLen})
		}
	}
	return out
}

// Patch returns a copy of the image with new current limits (mA) and all
// affected hashes recomputed. The result is parsed and verified again, and
// it may differ from the original only inside PatchRanges.
func (img *Image) Patch(chargeCurrent, inputCurrent uint32) ([]byte, error) {
	if err := img.Verify(); err != nil {
		return nil, err
	}
	if img.Signed {
		return nil, ErrSigned
	}
	out := bytes.Clone(img.Data)
	for _, c := range img.Copies {
		binary.BigEndian.PutUint32(out[c.Charger.ChargeOff:], chargeCurrent)
		binary.BigEndian.PutUint32(out[c.Charger.InputOff:], inputCurrent)
	}
	// Inner hashes first: the resource entry hash lies inside the resource
	// part, whose FIT hash covers it.
	for _, c := range img.Copies {
		if c.EntryHashLen > 0 {
			copy(out[c.EntryHashOff:c.EntryHashOff+c.EntryHashLen], sum(c.EntryHashAlgo, out[c.Offset:c.Offset+c.Size]))
		}
	}
	for _, p := range img.Parts {
		if p.HashLen > 0 {
			copy(out[p.HashOff:p.HashOff+p.HashLen], sum(p.HashAlgo, out[p.Offset:p.Offset+p.Size]))
		}
	}

	patched, err := Parse(out)
	if err != nil {
		return nil, fmt.Errorf("patched image: %w", err)
	}
	if err := patched.Verify(); err != nil {
		return nil, fmt.Errorf("patched image: %w", err)
	}
	if v := patched.Values(); v.ChargeCurrent != chargeCurrent || v.InputCurrent != inputCurrent ||
		v.ChargeVoltage != img.Values().ChargeVoltage {
		return nil, fmt.Errorf("patched image reads back %+v", v)
	}
	allowed := make([]bool, len(out))
	for _, r := range img.PatchRanges() {
		for i := r.Off; i < r.Off+r.Len; i++ {
			allowed[i] = true
		}
	}
	for i := range out {
		if out[i] != img.Data[i] && !allowed[i] {
			return nil, fmt.Errorf("patch changed byte %#x outside the charger values and hashes", i)
		}
	}
	return out, nil
}

// SHA256 is the hex sha256 of the whole image.
func (img *Image) SHA256() string {
	h := sha256.Sum256(img.Data)
	return hex.EncodeToString(h[:])
}

// BaseSHA256 is the hex sha256 of the image with PatchRanges zeroed: images
// that differ only in the charger mode share it, while another firmware
// (a different kernel or device tree) gets another one.
func (img *Image) BaseSHA256() string {
	h := sha256.New()
	pos := 0
	zero := make([]byte, 64)
	for _, r := range sortedRanges(img.PatchRanges()) {
		if r.Off < pos {
			continue // overlapping ranges do not occur, but never hash twice
		}
		h.Write(img.Data[pos:r.Off])
		for n := r.Len; n > 0; {
			k := min(n, len(zero))
			h.Write(zero[:k])
			n -= k
		}
		pos = r.Off + r.Len
	}
	h.Write(img.Data[pos:])
	return hex.EncodeToString(h.Sum(nil))
}

func sortedRanges(rs []Range) []Range {
	out := append([]Range(nil), rs...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Off < out[j-1].Off; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// HasCompatible reports whether the device tree's root is compatible with
// value (for example "rockchip,rk3568").
func (img *Image) HasCompatible(value string) bool {
	for _, s := range img.Copies[0].Compatible {
		if s == value {
			return true
		}
	}
	return false
}
