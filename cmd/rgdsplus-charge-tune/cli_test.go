package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rgdsplus-charge-tune/internal/bootimg"
	"rgdsplus-charge-tune/internal/platform"
)

func patchRig(t *testing.T) (*platform.Platform, string) {
	t.Helper()
	p, err := platform.Load(platform.Options{})
	if err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(t.TempDir(), "boot.img")
	if err := os.WriteFile(in, bootimg.Synth(bootimg.SynthOptions{}), 0o644); err != nil {
		t.Fatal(err)
	}
	return p, in
}

func TestPatchWritesNewFile(t *testing.T) {
	p, in := patchRig(t)
	out := filepath.Join(t.TempDir(), "patched.img")
	if err := os.WriteFile(out, []byte("old"), 0o644); err != nil { // a regular file is replaced
		t.Fatal(err)
	}
	if err := runPatch(p, in, out, "fast", 0, 0); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out)
	img, err := bootimg.Parse(data)
	if err != nil || img.Verify() != nil {
		t.Fatalf("patched image: %v", err)
	}
	if v := img.Values(); v.ChargeCurrent != 2500 || v.InputCurrent != 3000 {
		t.Fatalf("values %+v", v)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(out), ".*tmp*")); len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
}

func TestPatchRefusesUnsafeOutputs(t *testing.T) {
	p, in := patchRig(t)
	orig, _ := os.ReadFile(in)
	link := filepath.Join(t.TempDir(), "link.img")
	if err := os.Symlink(in, link); err != nil {
		t.Fatal(err)
	}
	devLink := filepath.Join(t.TempDir(), "boot")
	if err := os.Symlink(os.DevNull, devLink); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct{ out, want string }{
		"device node":         {os.DevNull, "not a regular file"},
		"symlink to a device": {devLink, "not a regular file"},
		"folder":              {t.TempDir(), "not a regular file"},
		"the input itself":    {in, "is the input file"},
		"a link to the input": {link, "is the input file"},
		"no output given":     {"", "-patch-out is required"},
	}
	for name, c := range cases {
		err := runPatch(p, in, c.out, "fast", 0, 0)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got, _ := os.ReadFile(in); string(got) != string(orig) {
		t.Fatal("the input changed")
	}
}
