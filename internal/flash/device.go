// Package flash reads and writes the boot partition and keeps its backups.
//
// A write touches only the 512-byte sectors that differ, then reads the
// whole partition back from the card (not from the page cache) and compares
// it with what was meant to be written.
package flash

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

const sectorSize = 512

// afterWrite is a test hook called after each write, before the partition
// is read back: attempt is 1 or 2 for the new data, attemptRestore for
// putting the previous content back.
var afterWrite func(path string, attempt int)

const attemptRestore = 0

// WriteError is a write of the boot partition that failed (Err: ErrVerify or
// an I/O error). The previous content was then written back; Restored says
// whether the partition reads back as it was. If not, it may hold a mix of
// old and new sectors.
type WriteError struct {
	Err      error
	Restored bool
}

func (e *WriteError) Error() string {
	if e.Restored {
		return e.Err.Error() + "; the previous content was put back"
	}
	return e.Err.Error() + "; the previous content could not be put back"
}

func (e *WriteError) Unwrap() error { return e.Err }

var (
	// ErrChanged means the partition no longer holds the data a write was
	// prepared from.
	ErrChanged = errors.New("the boot partition changed since it was read")
	// ErrVerify means the partition does not read back as written.
	ErrVerify = errors.New("the boot partition does not read back as written")
)

// Device is the boot partition: a block device on the console, a plain file
// on a development computer.
type Device struct {
	Path  string // resolved path (/dev/mmcblk1p3)
	Size  int64
	Block bool
}

// Open resolves path (the by-name symlink) and finds the partition size. A
// regular file is accepted only with allowFile (development).
func Open(path string, allowFile bool) (*Device, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	d := &Device{Path: resolved}
	switch {
	case st.Mode()&os.ModeDevice != 0 && st.Mode()&os.ModeCharDevice == 0:
		d.Block = true
		size, err := blockSize(resolved)
		if err != nil {
			return nil, fmt.Errorf("size of %s: %w", resolved, err)
		}
		d.Size = size
	case st.Mode().IsRegular() && allowFile:
		d.Size = st.Size()
	default:
		return nil, fmt.Errorf("%s is not a block device", resolved)
	}
	if d.Size <= 0 || d.Size%sectorSize != 0 {
		return nil, fmt.Errorf("%s: unexpected size %d", resolved, d.Size)
	}
	return d, nil
}

// Read returns the whole partition as it is on the card.
func (d *Device) Read() ([]byte, error) {
	f, err := os.Open(d.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if d.Block {
		dropBlockCache(f)
	}
	buf := make([]byte, d.Size)
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// WriteReport says what a write did.
type WriteReport struct {
	Sectors int // sectors written
	Runs    int // contiguous runs of them
}

// Write changes the partition from orig (what it holds now) to want,
// writing only the sectors that differ. The partition is read first to
// make sure it still holds orig, and read back afterwards; a mismatch is
// written once more. If that fails too, or a write or read fails, orig is
// written back and checked, and the error is a *WriteError. progress gets
// 0..1.
func (d *Device) Write(orig, want []byte, progress func(float64)) (WriteReport, error) {
	if progress == nil {
		progress = func(float64) {}
	}
	if int64(len(orig)) != d.Size || int64(len(want)) != d.Size {
		return WriteReport{}, fmt.Errorf("write of %d bytes over %d onto a %d-byte partition", len(want), len(orig), d.Size)
	}
	cur, err := d.Read()
	if err != nil {
		return WriteReport{}, err
	}
	if !bytes.Equal(cur, orig) {
		return WriteReport{}, ErrChanged
	}
	progress(0.2)
	runs := diffRuns(orig, want)
	var rep WriteReport
	for _, r := range runs {
		rep.Sectors += r.Len / sectorSize
	}
	rep.Runs = len(runs)
	if len(runs) == 0 {
		progress(1)
		return rep, nil
	}
	touched := runs
	fail := func(cause error) (WriteReport, error) {
		restored := d.restore(orig, touched)
		slog.Error("boot partition write failed", "err", cause, "restored", restored)
		return rep, &WriteError{Err: cause, Restored: restored}
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if err := d.writeRuns(want, runs); err != nil {
			return fail(err)
		}
		if afterWrite != nil {
			afterWrite(d.Path, attempt)
		}
		progress(0.5)
		back, err := d.Read()
		if err != nil {
			return fail(err)
		}
		if bytes.Equal(back, want) {
			progress(1)
			return rep, nil
		}
		slog.Warn("boot partition did not read back as written", "attempt", attempt,
			"want_sha256", fmt.Sprintf("%x", sha256.Sum256(want)), "got_sha256", fmt.Sprintf("%x", sha256.Sum256(back)))
		// The next attempt rewrites whatever still differs.
		runs = diffRuns(back, want)
	}
	return fail(ErrVerify)
}

// restore writes orig back over the sectors that differ from it (all the
// sectors a failed write touched when the partition cannot be read) and
// reports whether the partition then reads back as orig.
func (d *Device) restore(orig []byte, touched []run) bool {
	runs := touched
	if cur, err := d.Read(); err == nil {
		runs = diffRuns(cur, orig)
		if len(runs) == 0 {
			return true
		}
	}
	if err := d.writeRuns(orig, runs); err != nil {
		slog.Error("putting the boot partition back", "err", err)
		return false
	}
	if afterWrite != nil {
		afterWrite(d.Path, attemptRestore)
	}
	back, err := d.Read()
	return err == nil && bytes.Equal(back, orig)
}

type run struct{ Off, Len int }

// diffRuns lists the sector-aligned runs where a and b differ.
func diffRuns(a, b []byte) []run {
	var out []run
	for off := 0; off < len(a); off += sectorSize {
		end := min(off+sectorSize, len(a))
		if bytes.Equal(a[off:end], b[off:end]) {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Off+out[n-1].Len == off {
			out[n-1].Len += end - off
		} else {
			out = append(out, run{off, end - off})
		}
	}
	return out
}

func (d *Device) writeRuns(want []byte, runs []run) error {
	f, err := os.OpenFile(d.Path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	for _, r := range runs {
		if _, err := f.WriteAt(want[r.Off:r.Off+r.Len], int64(r.Off)); err != nil {
			f.Close()
			return fmt.Errorf("write at %#x: %w", r.Off, err)
		}
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if d.Block {
		dropBlockCache(f)
	}
	return f.Close()
}
