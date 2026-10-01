package tune

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rgdsplus-charge-tune/internal/bootimg"
	"rgdsplus-charge-tune/internal/charger"
	"rgdsplus-charge-tune/internal/platform"
)

type rig struct {
	t    *testing.T
	p    *platform.Platform
	e    *Engine
	dir  string // backup folder (host path)
	boot string // boot partition file (host path)
}

// newRig is a fake console under a temporary -root: stock-like boot
// partition, a live device tree booted with the given values, 80 % battery.
func newRig(t *testing.T, booted charger.Values, image []byte) *rig {
	t.Helper()
	p, err := platform.Load(platform.Options{Root: t.TempDir(), DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	p.BootSize = int64(len(image))
	r := &rig{t: t, p: p, e: New(p, "test"), dir: p.FSPath("/mnt/sdcard/rgdsplus-charge-tune/backups"), boot: p.BootPath()}
	r.e.Now = func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
	r.file(p.BoardFile, []byte("RGdsplus\n"))
	r.file(p.FirmwareFile, []byte("20260915"))
	r.file(p.DeviceTree+"/compatible", []byte("rockchip,rk3568\x00"))
	r.boot = p.BootPath()
	r.file(p.BootPartition, image)
	r.file(p.BatteryCapacityFile, []byte("80\n"))
	r.setRunning(booted)
	os.MkdirAll(p.FSPath("/mnt/sdcard"), 0o755)
	return r
}

func (r *rig) file(devicePath string, data []byte) {
	r.t.Helper()
	path := r.p.FSPath(devicePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) setRunning(v charger.Values) {
	node := r.p.DeviceTree + r.p.ChargerNode
	r.file(node+"/compatible", []byte("rk817,charger\x00"))
	r.file(node+"/max_chrg_current", binary.BigEndian.AppendUint32(nil, uint32(v.ChargeCurrent)))
	r.file(node+"/max_input_current", binary.BigEndian.AppendUint32(nil, uint32(v.InputCurrent)))
	r.file(node+"/max_chrg_voltage", binary.BigEndian.AppendUint32(nil, uint32(v.ChargeVoltage)))
}

func (r *rig) bootValues() charger.Values {
	r.t.Helper()
	b, err := r.e.ReadBoot()
	if err != nil || b.Image == nil {
		r.t.Fatalf("boot: %v %v", err, b.Err)
	}
	return b.Values()
}

func (r *rig) backups() []string {
	matches, _ := filepath.Glob(filepath.Join(r.dir, "boot-*.img"))
	return matches
}

var stock = charger.Values{ChargeCurrent: 2000, InputCurrent: 1500, ChargeVoltage: 4400}

func mode(p *platform.Platform, id string) charger.Mode {
	for _, m := range p.Modes {
		if m.ID == id {
			return m
		}
	}
	panic(id)
}

func TestApplyBacksUpOncePerFirmware(t *testing.T) {
	orig := bootimg.Synth(bootimg.SynthOptions{TotalSize: 1 << 20})
	r := newRig(t, stock, orig)

	res, err := r.e.Apply(mode(r.p, "fast"), r.dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.BackupCreated || !res.RebootNeeded || res.Written.Sectors == 0 {
		t.Fatalf("first apply %+v", res)
	}
	if v := r.bootValues(); v.ChargeCurrent != 2500 || v.InputCurrent != 3000 || v.ChargeVoltage != 4400 {
		t.Fatalf("boot holds %+v", v)
	}
	saved, err := os.ReadFile(res.Backup.Image)
	if err != nil || !bytes.Equal(saved, orig) {
		t.Fatalf("the backup is not the original image (%v)", err)
	}

	// Another mode: the image differs from the backup only in the mode, so
	// no second backup.
	res, err = r.e.Apply(mode(r.p, "gentle"), r.dir, nil)
	if err != nil || res.BackupCreated || len(r.backups()) != 1 {
		t.Fatalf("second apply %+v, %d backups, %v", res, len(r.backups()), err)
	}

	// Back to what the kernel booted with before a reboot: nothing to reboot.
	res, err = r.e.Apply(mode(r.p, "stock"), r.dir, nil)
	if err != nil || res.RebootNeeded {
		t.Fatalf("back to the booted mode %+v %v", res, err)
	}
	if got, _ := os.ReadFile(r.boot); !bytes.Equal(got, orig) {
		t.Fatal("stock mode does not give back the original image")
	}

	// The same mode again: nothing written.
	res, err = r.e.Apply(mode(r.p, "stock"), r.dir, nil)
	if err != nil || !res.Unchanged || res.RebootNeeded {
		t.Fatalf("same mode %+v %v", res, err)
	}
}

func TestNewFirmwareGetsItsOwnBackup(t *testing.T) {
	r := newRig(t, stock, bootimg.Synth(bootimg.SynthOptions{TotalSize: 1 << 20}))
	if _, err := r.e.Apply(mode(r.p, "fast"), r.dir, nil); err != nil {
		t.Fatal(err)
	}
	// A firmware update replaces the partition with another kernel.
	r.file(r.p.BootPartition, bootimg.Synth(bootimg.SynthOptions{TotalSize: 1 << 20, KernelSeed: 9}))
	res, err := r.e.Apply(mode(r.p, "fast"), r.dir, nil)
	if err != nil || !res.BackupCreated || len(r.backups()) != 2 {
		t.Fatalf("after the update %+v, %d backups, %v", res, len(r.backups()), err)
	}
	boot, _ := r.e.ReadBoot()
	infos := r.e.ListBackups(r.dir, boot.Base)
	same := 0
	for _, i := range infos {
		if i.Err != nil {
			t.Fatalf("%s: %v", i.Image, i.Err)
		}
		if i.SameFirmware {
			same++
		}
	}
	if len(infos) != 2 || same != 1 {
		t.Fatalf("backups %+v", infos)
	}
}

func TestRestore(t *testing.T) {
	orig := bootimg.Synth(bootimg.SynthOptions{TotalSize: 1 << 20})
	r := newRig(t, stock, orig)
	res, err := r.e.Apply(mode(r.p, "fast"), r.dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	// After a reboot the kernel runs the fast mode.
	r.setRunning(charger.Values{ChargeCurrent: 2500, InputCurrent: 3000, ChargeVoltage: 4400})

	back, err := r.e.Restore(res.Backup, r.dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if back.BackupCreated || !back.RebootNeeded {
		t.Fatalf("restore %+v", back)
	}
	if got, _ := os.ReadFile(r.boot); !bytes.Equal(got, orig) {
		t.Fatal("restore did not write the backup")
	}
	// Restoring what is already there does nothing.
	again, err := r.e.Restore(res.Backup, r.dir, nil)
	if err != nil || !again.Unchanged {
		t.Fatalf("second restore %+v %v", again, err)
	}
}

func TestRefusals(t *testing.T) {
	r := newRig(t, stock, bootimg.Synth(bootimg.SynthOptions{TotalSize: 1 << 20}))
	r.file(r.p.BatteryCapacityFile, []byte("10\n"))
	var low *platform.LowBatteryError
	if _, err := r.e.Apply(mode(r.p, "fast"), r.dir, nil); !errors.As(err, &low) {
		t.Fatalf("low battery: %v", err)
	}
	r.file(r.p.BatteryCapacityFile, []byte("80\n"))

	r.file(r.p.BoardFile, []byte("RG35XX\n"))
	var dev *platform.UnsupportedDeviceError
	if _, err := r.e.Apply(mode(r.p, "fast"), r.dir, nil); !errors.As(err, &dev) {
		t.Fatalf("other device: %v", err)
	}
	r.file(r.p.BoardFile, []byte("RGdsplus\n"))

	if _, err := r.e.Apply(charger.Mode{ID: "hot", ChargeCurrent: 3500, InputCurrent: 3000}, r.dir, nil); err == nil {
		t.Fatal("an unsafe mode was applied")
	}

	signed := bootimg.Synth(bootimg.SynthOptions{TotalSize: 1 << 20, Signed: true})
	r.file(r.p.BootPartition, signed)
	if _, err := r.e.Apply(mode(r.p, "fast"), r.dir, nil); !errors.Is(err, bootimg.ErrSigned) {
		t.Fatalf("signed image: %v", err)
	}

	damaged := bootimg.Synth(bootimg.SynthOptions{TotalSize: 1 << 20})
	damaged[0x900] ^= 0xff // inside the fdt part
	r.file(r.p.BootPartition, damaged)
	if _, err := r.e.Apply(mode(r.p, "fast"), r.dir, nil); !errors.Is(err, ErrNotApplicable) {
		t.Fatalf("damaged image: %v", err)
	}
	if len(r.backups()) != 0 {
		t.Fatal("a refused apply left a backup")
	}
	if got, _ := os.ReadFile(r.boot); !bytes.Equal(got, damaged) {
		t.Fatal("a refused apply changed the partition")
	}
}

// Writing is refused, with nothing written, when the backup folder is on
// the root filesystem (no card) or the partition has another size.
func TestRefusalsLeavePartitionAlone(t *testing.T) {
	orig := bootimg.Synth(bootimg.SynthOptions{TotalSize: 1 << 20})
	r := newRig(t, stock, orig)

	r.p.OnRootFS = func(string) (bool, error) { return true, nil }
	if _, err := r.e.Apply(mode(r.p, "fast"), r.dir, nil); !errors.Is(err, platform.ErrNotOnCard) {
		t.Fatalf("backup folder on the root filesystem: %v", err)
	}
	r.p.OnRootFS = nil

	r.p.BootSize = int64(len(orig)) + 512
	if _, err := r.e.Apply(mode(r.p, "fast"), r.dir, nil); err == nil || !strings.Contains(err.Error(), "expected") {
		t.Fatalf("partition of another size: %v", err)
	}

	if len(r.backups()) != 0 {
		t.Fatal("a refused write left a backup")
	}
	if got, _ := os.ReadFile(r.boot); !bytes.Equal(got, orig) {
		t.Fatal("a refused write changed the partition")
	}
}
