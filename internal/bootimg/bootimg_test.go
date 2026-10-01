package bootimg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

func mustParse(t *testing.T, data []byte) *Image {
	t.Helper()
	img, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestSynthParsesAndVerifies(t *testing.T) {
	img := mustParse(t, Synth(SynthOptions{TotalSize: 1 << 20}))
	if len(img.Data) != 1<<20 {
		t.Fatalf("image is %d bytes", len(img.Data))
	}
	if len(img.Parts) != 3 || len(img.Copies) != 2 {
		t.Fatalf("%d parts, %d device tree copies", len(img.Parts), len(img.Copies))
	}
	if img.Signed {
		t.Fatal("unsigned image reported as signed")
	}
	if err := img.Verify(); err != nil {
		t.Fatal(err)
	}
	if v := img.Values(); v != (Values{ChargeCurrent: 2000, InputCurrent: 1500, ChargeVoltage: 4400}) {
		t.Fatalf("values %+v", v)
	}
	if c := img.Copies[1]; c.Where != "resource/rk-kernel.dtb" || c.EntryHashAlgo != "sha1" || c.EntryHashLen != 20 {
		t.Fatalf("resource copy %+v", c)
	}
	if !img.HasCompatible("rockchip,rk3568") || img.HasCompatible("rockchip,rk3588") {
		t.Fatal("root compatible")
	}
	if img.Parts[0].Offset != synthDataStart {
		t.Fatalf("fdt at %#x", img.Parts[0].Offset)
	}
}

func TestPatchChangesOnlyValuesAndHashes(t *testing.T) {
	orig := Synth(SynthOptions{TotalSize: 1 << 20})
	img := mustParse(t, orig)
	out, err := img.Patch(2500, 3000)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(out, orig) {
		t.Fatal("patch changed nothing")
	}
	patched := mustParse(t, out)
	if err := patched.Verify(); err != nil {
		t.Fatal(err)
	}
	if v := patched.Values(); v != (Values{ChargeCurrent: 2500, InputCurrent: 3000, ChargeVoltage: 4400}) {
		t.Fatalf("values %+v", v)
	}
	// Changed bytes: 2 values x 2 copies, the SHA-1 in the resource entry
	// and the sha256 of the fdt and resource parts; the kernel hash stays.
	changed := 0
	for i := range out {
		if out[i] != orig[i] {
			changed++
		}
	}
	if changed > 4*4+20+2*32 {
		t.Fatalf("%d bytes changed", changed)
	}
	kernel := img.Parts[1]
	if !bytes.Equal(out[kernel.HashOff:kernel.HashOff+kernel.HashLen], orig[kernel.HashOff:kernel.HashOff+kernel.HashLen]) {
		t.Fatal("kernel hash changed")
	}
	// Back to the stock values: byte for byte the original.
	back, err := patched.Patch(2000, 1500)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back, orig) {
		t.Fatal("patching back does not restore the original")
	}
	if img.SHA256() == patched.SHA256() {
		t.Fatal("sha256 did not change")
	}
}

func TestBaseHashIgnoresModeOnly(t *testing.T) {
	orig := mustParse(t, Synth(SynthOptions{}))
	out, err := orig.Patch(1000, 2000)
	if err != nil {
		t.Fatal(err)
	}
	patched := mustParse(t, out)
	if orig.BaseSHA256() != patched.BaseSHA256() {
		t.Fatal("images differing only in the mode have different base hashes")
	}
	other := mustParse(t, Synth(SynthOptions{KernelSeed: 1}))
	if other.BaseSHA256() == orig.BaseSHA256() {
		t.Fatal("another kernel has the same base hash")
	}
	volt := mustParse(t, Synth(SynthOptions{ChargeVoltage: 4350}))
	if volt.BaseSHA256() == orig.BaseSHA256() {
		t.Fatal("another charge voltage has the same base hash")
	}
}

func TestVerifyFindsDamage(t *testing.T) {
	img := mustParse(t, Synth(SynthOptions{}))
	cases := map[string]int{
		"kernel":                 img.Parts[1].Offset + 100,
		"fdt":                    img.Copies[0].Offset + 8, // header byte of the fdt copy
		"resource/rk-kernel.dtb": img.Copies[1].Charger.ChargeOff,
		"resource":               img.Parts[2].Offset + img.Parts[2].Size - 1, // the logo
	}
	for want, off := range cases {
		data := bytes.Clone(img.Data)
		data[off] ^= 0xff
		dmg, err := Parse(data)
		if err != nil {
			continue // damage that breaks parsing is fine too
		}
		var hm *HashMismatchError
		if err := dmg.Verify(); !errors.As(err, &hm) || hm.Part != want {
			t.Errorf("%s: Verify = %v", want, err)
		}
		if _, err := dmg.Patch(2500, 3000); err == nil {
			t.Errorf("%s: a damaged image was patched", want)
		}
	}
}

func TestRefusals(t *testing.T) {
	signed := mustParse(t, Synth(SynthOptions{Signed: true}))
	if !signed.Signed {
		t.Fatal("signature value not seen")
	}
	if _, err := signed.Patch(2500, 3000); !errors.Is(err, ErrSigned) {
		t.Errorf("signed: %v", err)
	}

	var unsupported *UnsupportedError
	if _, err := Parse(Synth(SynthOptions{EntryHashSize: 16})); !errors.As(err, &unsupported) {
		t.Errorf("16-byte entry hash: %v", err)
	}

	if _, err := Parse(bytes.Repeat([]byte{0x5a}, 4096)); !errors.Is(err, ErrNotFIT) {
		t.Errorf("random data: %v", err)
	}
	dtbOnly := synthDTB(SynthOptions{ChargeCurrent: 2000, InputCurrent: 1500, ChargeVoltage: 4400})
	if _, err := Parse(dtbOnly); !errors.Is(err, ErrNotFIT) {
		t.Errorf("a plain device tree: %v", err)
	}

	// Both copies must be there: U-Boot may load either one.
	for name, o := range map[string]SynthOptions{
		"resource without rk-kernel.dtb": {NoResourceDTB: true},
		"resource part not recognised":   {ResourceName: "res"},
	} {
		if _, err := Parse(Synth(o)); !errors.Is(err, ErrMissingCopy) {
			t.Errorf("%s: %v", name, err)
		}
	}

	// A charger node without one of its values.
	for _, prop := range []string{PropChargeCurrent, PropInputCurrent, PropChargeVoltage} {
		if _, err := Parse(Synth(SynthOptions{OmitProperty: prop})); err == nil || !strings.Contains(err.Error(), prop) {
			t.Errorf("without %s: %v", prop, err)
		}
	}

	// One copy changed by hand: the copies disagree.
	img := mustParse(t, Synth(SynthOptions{}))
	data := bytes.Clone(img.Data)
	binary.BigEndian.PutUint32(data[img.Copies[0].Charger.ChargeOff:], 2500)
	if _, err := Parse(data); !errors.Is(err, ErrCopiesDiffer) {
		t.Errorf("differing copies: %v", err)
	}
}

func TestEntryHashVariants(t *testing.T) {
	for _, o := range []SynthOptions{{NoEntryHash: true}, {EntryHashSize: 20}, {EntryHashSize: 32}} {
		img := mustParse(t, Synth(o))
		out, err := img.Patch(2500, 3000)
		if err != nil {
			t.Fatalf("%+v: %v", o, err)
		}
		if err := mustParse(t, out).Verify(); err != nil {
			t.Fatalf("%+v: %v", o, err)
		}
	}
}

func TestPatchRangesLeaveVoltage(t *testing.T) {
	img := mustParse(t, Synth(SynthOptions{}))
	for _, r := range img.PatchRanges() {
		for _, c := range img.Copies {
			if c.Charger.VoltageOff >= r.Off && c.Charger.VoltageOff < r.Off+r.Len {
				t.Fatalf("charge voltage of %s is patchable", c.Where)
			}
		}
	}
}
