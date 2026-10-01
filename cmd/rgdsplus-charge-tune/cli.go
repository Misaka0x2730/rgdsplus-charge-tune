package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"rgdsplus-charge-tune/internal/bootimg"
	"rgdsplus-charge-tune/internal/charger"
	"rgdsplus-charge-tune/internal/platform"
	"rgdsplus-charge-tune/internal/store"
	"rgdsplus-charge-tune/internal/tune"
	"rgdsplus-charge-tune/internal/version"
)

type statusJSON struct {
	Version   string          `json:"version"`
	Board     string          `json:"board"`
	Firmware  string          `json:"firmware"`
	Device    string          `json:"device"` // "ok" or why the device is not supported
	Running   *charger.Values `json:"running,omitempty"`
	RunErr    string          `json:"running_error,omitempty"`
	Boot      *bootJSON       `json:"boot,omitempty"`
	BootErr   string          `json:"boot_error,omitempty"`
	Pending   bool            `json:"reboot_pending"` // boot values differ from the running ones
	Modes     []charger.Mode  `json:"modes"`
	BackupDir string          `json:"backup_dir"`
	Backups   []backupJSON    `json:"backups"`
	Power     platform.Power  `json:"power"`
}

type bootJSON struct {
	Path   string         `json:"path"`
	Size   int64          `json:"size"`
	SHA256 string         `json:"sha256"`
	Base   string         `json:"base_sha256"`
	Values charger.Values `json:"values"`
	Mode   string         `json:"mode,omitempty"`
	Signed bool           `json:"signed"`
	Copies []copyJSON     `json:"copies,omitempty"`
	Error  string         `json:"error,omitempty"`
}

type copyJSON struct {
	Where      string `json:"where"`
	Offset     string `json:"offset"`
	ChargeOff  string `json:"charge_offset"`
	InputOff   string `json:"input_offset"`
	EntryHash  string `json:"entry_hash,omitempty"`
	ChargerDTB string `json:"charger_node"`
}

type backupJSON struct {
	Path         string         `json:"path"`
	SHA256       string         `json:"sha256"`
	Firmware     string         `json:"firmware"`
	Created      string         `json:"created"`
	Values       charger.Values `json:"values"`
	SameFirmware bool           `json:"same_firmware"`
	Error        string         `json:"error,omitempty"`
}

// runStatus prints what the app sees. It only reads.
func runStatus(p *platform.Platform, data string) error {
	settings, _ := store.LoadSettings(data)
	backupDir := p.DefaultBackupDir()
	if settings.BackupDir != "" {
		backupDir = p.FSPath(settings.BackupDir)
	}
	out := statusJSON{Version: version.String(), Board: p.Board(), Firmware: p.Firmware(), Device: "ok",
		Modes: p.Modes, BackupDir: p.DevicePath(backupDir), Power: p.PowerState()}
	if err := p.CheckDevice(); err != nil {
		out.Device = err.Error()
	}
	if v, err := p.RunningValues(); err != nil {
		out.RunErr = err.Error()
	} else {
		out.Running = &v
	}
	e := tune.New(p, version.Version)
	boot, err := e.ReadBoot()
	base := ""
	if err != nil {
		out.BootErr = err.Error()
	} else {
		b := &bootJSON{Path: boot.Dev.Path, Size: boot.Dev.Size, SHA256: boot.SHA256, Base: boot.Base}
		base = boot.Base
		if boot.Err != nil {
			b.Error = boot.Err.Error()
		} else {
			b.Values, b.Signed = boot.Values(), boot.Image.Signed
			if m, ok := charger.Find(p.Modes, b.Values); ok {
				b.Mode = m.ID
			}
			for _, c := range boot.Image.Copies {
				cj := copyJSON{Where: c.Where, Offset: hex(c.Offset), ChargeOff: hex(c.Charger.ChargeOff),
					InputOff: hex(c.Charger.InputOff), ChargerDTB: c.Charger.Path}
				if c.EntryHashLen > 0 {
					cj.EntryHash = c.EntryHashAlgo + "@" + hex(c.EntryHashOff)
				}
				b.Copies = append(b.Copies, cj)
			}
			out.Pending = out.Running != nil && !out.Running.Same(b.Values)
		}
		out.Boot = b
	}
	for _, b := range e.ListBackups(backupDir, base) {
		bj := backupJSON{Path: p.DevicePath(b.Image), SHA256: b.Meta.SHA256, Firmware: b.Meta.Firmware,
			Created: b.Meta.Created.Format("2006-01-02T15:04:05Z07:00"), Values: b.Meta.Values, SameFirmware: b.SameFirmware}
		if b.Err != nil {
			bj.Error = b.Err.Error()
		}
		out.Backups = append(out.Backups, bj)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// runPatch patches an image file offline, for development and for people
// who flash the partition themselves. Both files must be regular files: it
// never writes a partition (the app does that, with its backup and checks)
// and never overwrites its input, which may be a backup.
func runPatch(p *platform.Platform, in, out, modeID string, charge, input int) error {
	if out == "" {
		return errors.New("-patch-out is required")
	}
	m := charger.Mode{ID: "custom", ChargeCurrent: charge, InputCurrent: input}
	if modeID != "" {
		found := false
		for _, x := range p.Modes {
			if x.ID == modeID {
				m, found = x, true
			}
		}
		if !found {
			return fmt.Errorf("no mode %q", modeID)
		}
	}
	if err := m.Validate(); err != nil {
		return err
	}
	st, err := os.Stat(in)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", in)
	}
	if err := checkPatchOut(out, st); err != nil {
		return err
	}
	data, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	img, err := bootimg.Parse(data)
	if err != nil {
		return err
	}
	patched, err := img.Patch(uint32(m.ChargeCurrent), uint32(m.InputCurrent))
	if err != nil {
		return err
	}
	if err := writeNewFile(out, patched); err != nil {
		return err
	}
	after, _ := bootimg.Parse(patched)
	fmt.Printf("%s: charge %d mA, input %d mA (was %d / %d), sha256 %s\n", out, m.ChargeCurrent, m.InputCurrent,
		img.Values().ChargeCurrent, img.Values().InputCurrent, after.SHA256())
	return nil
}

// checkPatchOut accepts a file that does not exist yet or a regular file
// other than the input (symlinks followed): never a device, /dev/null or a
// folder.
func checkPatchOut(out string, in os.FileInfo) error {
	st, err := os.Stat(out)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file: -patch-out only writes image files, never a partition", out)
	}
	if os.SameFile(st, in) {
		return fmt.Errorf("%s is the input file: write the patched image to another file", out)
	}
	return nil
}

// writeNewFile writes data to a temporary file next to path and renames it
// over path, so path is either the old file or the complete new one.
func writeNewFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func hex(n int) string { return fmt.Sprintf("%#x", n) }
