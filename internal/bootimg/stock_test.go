package bootimg

import (
	"bytes"
	"os"
	"testing"
)

// The stock boot partition of firmware 20260915 (a dump of mmcblk1p3).
// The image is Anbernic's and not in the repository: point
// CHARGETUNE_STOCK_BOOT at a dump (`task test:stock STOCK_BOOT=...`).
const stockSHA256 = "eba3e65d9e7cc555849c49615024aef463bfaed277cd17484bcb6cf3350d7fa9"

func TestStockImage(t *testing.T) {
	path := os.Getenv("CHARGETUNE_STOCK_BOOT")
	if path == "" {
		t.Skip("CHARGETUNE_STOCK_BOOT not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img := mustParse(t, data)
	if got := img.SHA256(); got != stockSHA256 {
		t.Fatalf("not the stock 20260915 image: sha256 %s", got)
	}
	if len(data) != 64<<20 || img.Signed {
		t.Fatalf("%d bytes, signed %v", len(data), img.Signed)
	}
	if err := img.Verify(); err != nil {
		t.Fatal(err)
	}
	if v := img.Values(); v != (Values{ChargeCurrent: 2000, InputCurrent: 1500, ChargeVoltage: 4400}) {
		t.Fatalf("values %+v", v)
	}
	// The offsets found by hand while studying the image.
	fdtCopy, resCopy := img.Copies[0], img.Copies[1]
	checks := []struct {
		name      string
		got, want int
	}{
		{"fdt input", fdtCopy.Charger.InputOff, 0x749C},
		{"fdt charge", fdtCopy.Charger.ChargeOff, 0x74AC},
		{"fdt voltage", fdtCopy.Charger.VoltageOff, 0x74BC},
		{"resource input", resCopy.Charger.InputOff, 0x25D969C},
		{"resource charge", resCopy.Charger.ChargeOff, 0x25D96AC},
		{"fdt part hash", img.Parts[0].HashOff, 0x154},
		{"kernel part hash", img.Parts[1].HashOff, 0x244},
		{"resource part hash", img.Parts[2].HashOff, 0x304},
		{"fdt data", img.Parts[0].Offset, 0x800},
		{"resource data", img.Parts[2].Offset, 0x25D1200},
		{"resource dtb", resCopy.Offset, 0x25D1200 + 0x1800},
		{"resource dtb hash", resCopy.EntryHashOff, 0x25D1200 + 0x200 + 224},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s at %#x, want %#x", c.name, c.got, c.want)
		}
	}
	if resCopy.EntryHashAlgo != "sha1" {
		t.Errorf("resource entry hash %q", resCopy.EntryHashAlgo)
	}

	out, err := img.Patch(2500, 3000)
	if err != nil {
		t.Fatal(err)
	}
	patched := mustParse(t, out)
	if err := patched.Verify(); err != nil {
		t.Fatal(err)
	}
	if patched.BaseSHA256() != img.BaseSHA256() {
		t.Fatal("base hash changed with the mode")
	}
	back, err := patched.Patch(2000, 1500)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back, data) {
		t.Fatal("stock values do not give back the stock image")
	}
	if dir := os.Getenv("CHARGETUNE_PATCHED_OUT"); dir != "" {
		if err := os.WriteFile(dir, out, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
