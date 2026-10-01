package fdt

import (
	"encoding/binary"
	"errors"
	"testing"
)

func sample() []byte {
	b := NewBuilder()
	b.Begin("")
	b.PropStrings("compatible", "rockchip,rk3568-deep-lp3-v10", "rockchip,rk3568")
	b.Begin("i2c@fdd40000")
	b.Begin("pmic@20")
	b.PropStrings("compatible", "rockchip,rk817")
	b.Begin("charger")
	b.PropStrings("compatible", "rk817,charger")
	b.PropU32("max_input_current", 1500)
	b.PropU32("max_chrg_current", 2000)
	b.PropU32("max_chrg_voltage", 4400)
	b.Prop("empty", nil)
	b.End()
	b.End()
	b.End()
	b.End()
	return b.Bytes()
}

func TestParseBuiltTree(t *testing.T) {
	data := sample()
	// Trailing bytes after the blob (a partition image) are ignored.
	tree, err := Parse(append(append([]byte{}, data...), 0xff, 0xff, 0xff, 0xff))
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Data) != len(data) {
		t.Fatalf("tree data %d bytes, blob %d", len(tree.Data), len(data))
	}
	if got := tree.Strings(mustProp(t, tree.Root, "compatible")); len(got) != 2 || got[1] != "rockchip,rk3568" {
		t.Fatalf("root compatible = %q", got)
	}
	n := tree.Find("/i2c@fdd40000/pmic@20/charger")
	if n == nil || n.Path != "/i2c@fdd40000/pmic@20/charger" {
		t.Fatalf("charger node: %+v", n)
	}
	v, p, err := tree.PropU32(n, "max_chrg_current")
	if err != nil || v != 2000 {
		t.Fatalf("max_chrg_current = %d, %v", v, err)
	}
	if got := binary.BigEndian.Uint32(data[p.Offset:]); got != 2000 {
		t.Fatalf("offset %#x holds %d, not the value", p.Offset, got)
	}
	if found := tree.FindCompatible("rk817,charger"); len(found) != 1 || found[0] != n {
		t.Fatalf("FindCompatible = %v", found)
	}
	if e := mustProp(t, n, "empty"); e.Len != 0 {
		t.Fatalf("empty prop len %d", e.Len)
	}
	if _, _, err := tree.PropU32(n, "compatible"); err == nil {
		t.Fatal("a string list read as a cell")
	}
	if tree.Find("/nope") != nil || tree.Find("/i2c@fdd40000/nope") != nil {
		t.Fatal("found a missing node")
	}
	if tree.Find("/") != tree.Root {
		t.Fatal("root path")
	}
}

func TestParseRejectsDamage(t *testing.T) {
	good := sample()
	cases := map[string]func([]byte) []byte{
		"bad magic":   func(b []byte) []byte { b[0] = 0; return b },
		"too short":   func(b []byte) []byte { return b[:20] },
		"total > len": func(b []byte) []byte { binary.BigEndian.PutUint32(b[4:], uint32(len(b)+4)); return b },
		"old version": func(b []byte) []byte { binary.BigEndian.PutUint32(b[20:], 3); return b },
		"struct outside": func(b []byte) []byte {
			binary.BigEndian.PutUint32(b[36:], uint32(len(b)))
			return b
		},
		"unknown token": func(b []byte) []byte {
			off := binary.BigEndian.Uint32(b[8:])
			binary.BigEndian.PutUint32(b[off:], 0x77)
			return b
		},
		"no end token": func(b []byte) []byte {
			off := binary.BigEndian.Uint32(b[8:])
			size := binary.BigEndian.Uint32(b[36:])
			binary.BigEndian.PutUint32(b[36:], size-4)
			_ = off
			return b
		},
	}
	for name, damage := range cases {
		b := damage(append([]byte{}, good...))
		if _, err := Parse(b); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
	if _, err := Parse([]byte("hello world, this is not a device tree blob")); !errors.Is(err, ErrNotFDT) {
		t.Errorf("text: %v", err)
	}
}

func mustProp(t *testing.T, n *Node, name string) Prop {
	t.Helper()
	p, ok := n.Prop(name)
	if !ok {
		t.Fatalf("%s has no %s", n.Path, name)
	}
	return p
}
