// Package tune carries out what the user asks for: reading the state of
// the boot partition, applying a mode and restoring a backup. The UI and
// the command line both go through it.
package tune

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"rgdsplus-charge-tune/internal/bootimg"
	"rgdsplus-charge-tune/internal/charger"
	"rgdsplus-charge-tune/internal/flash"
	"rgdsplus-charge-tune/internal/platform"
)

// Engine holds what the operations share.
type Engine struct {
	P        *platform.Platform
	App      string           // app version, recorded in backups
	Now      func() time.Time // clock (tests)
	verifier flash.Verifier
}

// New returns an engine for the platform.
func New(p *platform.Platform, appVersion string) *Engine {
	return &Engine{P: p, App: appVersion, Now: time.Now}
}

// Boot is the boot partition as read now.
type Boot struct {
	Dev    *flash.Device
	Data   []byte
	SHA256 string
	// Image is nil when the partition is not an image this app understands
	// (Err says why); a restore still works then.
	Image *bootimg.Image
	Base  string // bootimg base hash; "sha256:<SHA256>" without Image
	Err   error
}

// Values of the image (zero without one).
func (b *Boot) Values() charger.Values {
	if b.Image == nil {
		return charger.Values{}
	}
	return toValues(b.Image.Values())
}

func toValues(v bootimg.Values) charger.Values {
	return charger.Values{ChargeCurrent: int(v.ChargeCurrent), InputCurrent: int(v.InputCurrent), ChargeVoltage: int(v.ChargeVoltage)}
}

// ReadBoot reads and checks the boot partition. The error is for the
// partition itself (missing, wrong size); a partition that is not a valid
// image is returned with Boot.Err set.
func (e *Engine) ReadBoot() (*Boot, error) {
	dev, err := flash.Open(e.P.BootPath(), e.P.Root != "" || e.P.Dev)
	if err != nil {
		return nil, err
	}
	if e.P.BootSize > 0 && dev.Size != e.P.BootSize {
		return nil, fmt.Errorf("the boot partition is %d bytes, expected %d", dev.Size, e.P.BootSize)
	}
	data, err := dev.Read()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	b := &Boot{Dev: dev, Data: data, SHA256: hex.EncodeToString(sum[:])}
	b.Base = "sha256:" + b.SHA256
	img, err := bootimg.Parse(data)
	if err == nil {
		err = img.Verify()
	}
	if err == nil && e.P.SoCCompatible != "" && !img.HasCompatible(e.P.SoCCompatible) {
		err = fmt.Errorf("the boot image is not for %s", e.P.SoCCompatible)
	}
	if err != nil {
		b.Err = err
		return b, nil
	}
	b.Image, b.Base = img, img.BaseSHA256()
	return b, nil
}

// Step is a stage of a write, for progress display.
type Step int

// Steps of Apply and Restore.
const (
	StepRead Step = iota
	StepBackup
	StepWrite
)

// Progress reports the current step and how far it is (0..1).
type Progress func(step Step, fraction float64)

// Result describes a finished write.
type Result struct {
	Backup        flash.Backup // the backup of the previous content
	BackupCreated bool         // made now (false: an existing one covered it)
	Written       flash.WriteReport
	Values        charger.Values // now in the boot partition
	RebootNeeded  bool
	Unchanged     bool // the partition already held the target
}

// ErrNotApplicable means the boot partition is not an image the app can
// change (see Boot.Err).
var ErrNotApplicable = errors.New("the boot partition cannot be changed")

func (e *Engine) preflight() error {
	if err := e.P.CheckDevice(); err != nil {
		return err
	}
	return e.P.CheckPower()
}

// Apply writes a mode into the boot partition after making sure a backup of
// the current firmware exists in (or is saved to) backupDir.
func (e *Engine) Apply(mode charger.Mode, backupDir string, progress Progress) (Result, error) {
	progress = orNop(progress)
	if err := mode.Validate(); err != nil {
		return Result{}, err
	}
	if err := e.preflight(); err != nil {
		return Result{}, err
	}
	progress(StepRead, 0)
	boot, err := e.ReadBoot()
	if err != nil {
		return Result{}, err
	}
	if boot.Image == nil {
		return Result{}, fmt.Errorf("%w: %v", ErrNotApplicable, boot.Err)
	}
	if boot.Image.Signed {
		return Result{}, bootimg.ErrSigned
	}
	progress(StepRead, 1)
	running, runErr := e.P.RunningValues()
	target := charger.Values{ChargeCurrent: mode.ChargeCurrent, InputCurrent: mode.InputCurrent, ChargeVoltage: boot.Values().ChargeVoltage}

	res := Result{Values: target}
	if boot.Values().Same(target) {
		res.Unchanged = true
		res.RebootNeeded = runErr != nil || !running.Same(target)
		return res, nil
	}
	backup, created, err := e.ensureBackup(boot, backupDir, progress)
	if err != nil {
		return Result{}, err
	}
	res.Backup, res.BackupCreated = backup, created
	patched, err := boot.Image.Patch(uint32(mode.ChargeCurrent), uint32(mode.InputCurrent))
	if err != nil {
		return Result{}, err
	}
	rep, err := boot.Dev.Write(boot.Data, patched, func(f float64) { progress(StepWrite, f) })
	res.Written = rep
	if err != nil {
		return res, err
	}
	res.RebootNeeded = runErr != nil || !running.Same(target)
	slog.Info("mode written", "mode", mode.ID, "charge_ma", mode.ChargeCurrent, "input_ma", mode.InputCurrent,
		"sectors", rep.Sectors, "runs", rep.Runs, "before_sha256", boot.SHA256,
		"after_sha256", fmt.Sprintf("%x", sha256.Sum256(patched)), "backup", backup.Image, "backup_created", created,
		"reboot_needed", res.RebootNeeded)
	return res, nil
}

// Restore writes a backup back. If the current content is not covered by a
// backup yet, it is saved first, so a restore can be undone too.
func (e *Engine) Restore(b flash.Backup, backupDir string, progress Progress) (Result, error) {
	progress = orNop(progress)
	if err := e.preflight(); err != nil {
		return Result{}, err
	}
	progress(StepRead, 0)
	data, err := b.Read()
	if err != nil {
		return Result{}, err
	}
	img, err := bootimg.Parse(data)
	if err == nil {
		err = img.Verify()
	}
	if err == nil && e.P.SoCCompatible != "" && !img.HasCompatible(e.P.SoCCompatible) {
		err = fmt.Errorf("the backup is not for %s", e.P.SoCCompatible)
	}
	if err != nil {
		return Result{}, fmt.Errorf("the backup is not a valid boot image: %w", err)
	}
	boot, err := e.ReadBoot()
	if err != nil {
		return Result{}, err
	}
	if int64(len(data)) != boot.Dev.Size {
		return Result{}, fmt.Errorf("the backup is %d bytes, the partition %d", len(data), boot.Dev.Size)
	}
	progress(StepRead, 1)
	running, runErr := e.P.RunningValues()
	values := toValues(img.Values())
	res := Result{Values: values}
	if boot.SHA256 == b.Meta.SHA256 {
		res.Unchanged = true
		res.RebootNeeded = runErr != nil || !running.Same(values)
		return res, nil
	}
	backup, created, err := e.ensureBackup(boot, backupDir, progress)
	if err != nil {
		return Result{}, err
	}
	res.Backup, res.BackupCreated = backup, created
	rep, err := boot.Dev.Write(boot.Data, data, func(f float64) { progress(StepWrite, f) })
	res.Written = rep
	if err != nil {
		return res, err
	}
	// Without a reboot the kernel keeps what it booted with: fine only if
	// that is the same firmware with the same values.
	res.RebootNeeded = runErr != nil || !running.Same(values) || img.BaseSHA256() != boot.Base
	slog.Info("backup restored", "backup", b.Image, "sha256", b.Meta.SHA256, "before_sha256", boot.SHA256,
		"sectors", rep.Sectors, "runs", rep.Runs, "reboot_needed", res.RebootNeeded)
	return res, nil
}

// ensureBackup finds a verified backup of the current firmware (same base
// hash) in the search folders, or saves the current content to dir.
func (e *Engine) ensureBackup(boot *Boot, dir string, progress Progress) (flash.Backup, bool, error) {
	backups, _ := flash.List(e.P.BackupSearchDirs(dir))
	if b, ok := e.verifier.Covering(backups, boot.Base); ok {
		return b, false, nil
	}
	if err := e.P.CheckBackupDir(dir); err != nil {
		return flash.Backup{}, false, err
	}
	meta := flash.Meta{
		BaseSHA256: boot.Base,
		Firmware:   e.P.Firmware(),
		Board:      e.P.Board(),
		Source:     boot.Dev.Path,
		App:        e.App,
		Values:     boot.Values(),
	}
	b, err := flash.Save(dir, boot.Data, meta, e.Now(), func(f float64) { progress(StepBackup, f) })
	if err != nil {
		return flash.Backup{}, false, fmt.Errorf("backup: %w", err)
	}
	slog.Info("backup saved", "path", b.Image, "sha256", b.Meta.SHA256, "base", b.Meta.BaseSHA256)
	return b, true, nil
}

// Backups lists the backups in the search folders with their check result.
type BackupInfo struct {
	flash.Backup
	Err          error // nil: verified
	SameFirmware bool  // same base hash as the current boot partition
}

// ListBackups lists and verifies the backups (hashing each file once per
// session), newest first. base is the current partition's base hash.
func (e *Engine) ListBackups(dir, base string) []BackupInfo {
	backups, problems := flash.List(e.P.BackupSearchDirs(dir))
	for _, p := range problems {
		slog.Warn("backup list", "err", p)
	}
	out := make([]BackupInfo, 0, len(backups))
	for _, b := range backups {
		out = append(out, BackupInfo{Backup: b, Err: e.verifier.Verify(b), SameFirmware: b.Meta.BaseSHA256 == base})
	}
	return out
}

func orNop(p Progress) Progress {
	if p == nil {
		return func(Step, float64) {}
	}
	return p
}
