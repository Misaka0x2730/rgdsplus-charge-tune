package bootimg

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"rgdsplus-charge-tune/internal/fdt"
)

// SynthOptions describe a synthetic boot image laid out like the RG DS Plus
// one (FIT with external data at 0x800, parts aligned to 512 bytes, a
// resource image with rk-kernel.dtb and a logo). Tests and the development
// device use it; it holds no Anbernic code.
type SynthOptions struct {
	ChargeCurrent uint32 // mA (default 2000)
	InputCurrent  uint32 // mA (default 1500)
	ChargeVoltage uint32 // mV (default 4400)
	KernelSize    int    // bytes of fake kernel (default 64 KiB)
	KernelSeed    byte   // varies the kernel bytes: another "firmware"
	TotalSize     int    // pad the image to this size (a partition); 0: no padding
	EntryHashSize int    // rk-kernel.dtb hash in the resource entry: 0, 20 (default) or 32
	NoEntryHash   bool   // force EntryHashSize 0
	Signed        bool   // give the configuration a signature value
	// Variants for refusal tests.
	NoResourceDTB bool   // a resource image without rk-kernel.dtb
	ResourceName  string // name the resource part differently (type "firmware")
	OmitProperty  string // leave this property out of the charger node
}

const synthDataStart = 0x800

// Synth builds the image.
func Synth(o SynthOptions) []byte {
	if o.ChargeCurrent == 0 {
		o.ChargeCurrent = 2000
	}
	if o.InputCurrent == 0 {
		o.InputCurrent = 1500
	}
	if o.ChargeVoltage == 0 {
		o.ChargeVoltage = 4400
	}
	if o.KernelSize == 0 {
		o.KernelSize = 64 << 10
	}
	if o.EntryHashSize == 0 && !o.NoEntryHash {
		o.EntryHashSize = sha1.Size
	}
	if o.NoEntryHash {
		o.EntryHashSize = 0
	}

	dtb := synthDTB(o)
	resDTB := dtb
	if o.NoResourceDTB {
		resDTB = nil
	}
	kernel := make([]byte, o.KernelSize)
	for i := range kernel {
		kernel[i] = byte(i*7) ^ o.KernelSeed
	}
	resource := synthResource(resDTB, o.EntryHashSize)

	type part struct {
		name, typ string
		data      []byte
		pos       int
	}
	resName, resType := "resource", "multi"
	if o.ResourceName != "" {
		resName, resType = o.ResourceName, "firmware"
	}
	parts := []*part{{"fdt", "flat_dt", dtb, 0}, {"kernel", "kernel", kernel, 0}, {resName, resType, resource, 0}}
	header := func() []byte {
		b := fdt.NewBuilder()
		b.Begin("")
		b.PropU32("timestamp", 0x6aa8e234)
		b.PropStrings("description", "U-Boot FIT source file for arm")
		b.Begin("images")
		for _, p := range parts {
			b.Begin(p.name)
			b.PropU32("data-size", uint32(len(p.data)))
			b.PropU32("data-position", uint32(p.pos))
			b.PropStrings("type", p.typ)
			b.PropStrings("arch", "arm64")
			b.PropStrings("compression", "none")
			b.Begin("hash")
			b.Prop("value", make([]byte, sha256.Size))
			b.PropStrings("algo", "sha256")
			b.End()
			b.End()
		}
		b.End()
		b.Begin("configurations")
		b.PropStrings("default", "conf")
		b.Begin("conf")
		b.PropStrings("fdt", "fdt")
		b.PropStrings("kernel", "kernel")
		b.PropStrings("multi", resName)
		b.Begin("signature")
		b.PropStrings("algo", "sha256,rsa2048")
		b.PropStrings("padding", "pss")
		b.PropStrings("key-name-hint", "dev")
		b.PropStrings("sign-images", "fdt", "kernel", "multi")
		if o.Signed {
			b.Prop("value", make([]byte, 256))
		}
		b.End()
		b.End()
		b.End()
		b.End()
		return b.Bytes()
	}
	pos := max(synthDataStart, align(len(header()), rsceBlock))
	for _, p := range parts {
		p.pos = pos
		pos = align(pos+len(p.data), rsceBlock)
	}
	hdr := header()
	if len(hdr) > parts[0].pos {
		panic(fmt.Sprintf("synthetic FIT header of %d bytes does not fit before %#x", len(hdr), parts[0].pos))
	}
	size := max(pos, o.TotalSize)
	img := make([]byte, size)
	copy(img, hdr)
	for _, p := range parts {
		copy(img[p.pos:], p.data)
	}
	// Fill in the part hashes now that the data is in place.
	tree, err := fdt.Parse(img)
	if err != nil {
		panic(err)
	}
	for _, p := range parts {
		v, _ := tree.Find("/images/" + p.name + "/hash").Prop("value")
		h := sha256.Sum256(p.data)
		copy(img[v.Offset:], h[:])
	}
	return img
}

func synthDTB(o SynthOptions) []byte {
	b := fdt.NewBuilder()
	b.Begin("")
	b.PropStrings("compatible", "rockchip,rk3568-deep-lp3-v10", "rockchip,rk3568")
	b.PropStrings("model", "Rockchip RK3568 DEEP LP3 V10 Board")
	b.Begin("i2c@fdd40000")
	b.Begin("pmic@20")
	b.PropStrings("compatible", "rockchip,rk817")
	b.Begin("charger")
	b.PropStrings("compatible", ChargerCompatible)
	b.PropU32("min_input_voltage", 4500)
	for _, p := range []struct {
		name  string
		value uint32
	}{{PropInputCurrent, o.InputCurrent}, {PropChargeCurrent, o.ChargeCurrent}, {PropChargeVoltage, o.ChargeVoltage}} {
		if p.name != o.OmitProperty {
			b.PropU32(p.name, p.value)
		}
	}
	b.PropU32("sample_res", 10)
	b.End()
	b.End()
	b.End()
	b.End()
	return b.Bytes()
}

// synthResource builds a resource image: header block, one table block per
// entry (rk-kernel.dtb unless dtb is nil, logo.bmp), then the files.
func synthResource(dtb []byte, hashSize int) []byte {
	logo := make([]byte, 3000)
	for i := range logo {
		logo[i] = byte(i)
	}
	type file struct {
		name string
		data []byte
	}
	files := []file{{"logo.bmp", logo}}
	if dtb != nil {
		files = append([]file{{ResourceDTBName, dtb}}, files...)
	}
	blk := 1 + len(files) // header + table
	out := make([]byte, blk*rsceBlock)
	copy(out, "RSCE")
	out[8], out[9], out[10] = 1, 1, 1
	binary.LittleEndian.PutUint32(out[12:], uint32(len(files)))
	for i, f := range files {
		e := out[(1+i)*rsceBlock:]
		copy(e, "ENTR")
		copy(e[4:], f.name)
		if f.name == ResourceDTBName && hashSize > 0 {
			var h []byte
			if hashSize == sha1.Size {
				s := sha1.Sum(f.data)
				h = s[:]
			} else {
				s := sha256.Sum256(f.data)
				h = s[:]
			}
			copy(e[rsceHashOff:], h[:min(hashSize, len(h))])
			binary.LittleEndian.PutUint32(e[rsceHashSizeOff:], uint32(hashSize))
		}
		start := len(out) / rsceBlock
		binary.LittleEndian.PutUint32(e[rsceBlkOff:], uint32(start))
		binary.LittleEndian.PutUint32(e[rsceSizeOff:], uint32(len(f.data)))
		padded := make([]byte, align(len(f.data), rsceBlock))
		copy(padded, f.data)
		out = append(out, padded...)
	}
	return out
}

func align(n, to int) int { return (n + to - 1) / to * to }
