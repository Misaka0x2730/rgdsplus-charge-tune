package flash

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rgdsplus-charge-tune/internal/charger"
	"rgdsplus-charge-tune/internal/platform"
)

func tempDevice(t *testing.T, data []byte) *Device {
	t.Helper()
	path := filepath.Join(t.TempDir(), "boot")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "by-name-boot")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	d, err := Open(link, true)
	if err != nil {
		t.Fatal(err)
	}
	if d.Path != mustEval(t, path) || d.Size != int64(len(data)) || d.Block {
		t.Fatalf("device %+v", d)
	}
	return d
}

func mustEval(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func image(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 31)
	}
	return b
}

func TestOpenRejectsFilesOnDevice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boot")
	os.WriteFile(path, image(4096), 0o644)
	if _, err := Open(path, false); err == nil {
		t.Fatal("a regular file accepted as the partition")
	}
	os.WriteFile(path, image(1000), 0o644)
	if _, err := Open(path, true); err == nil {
		t.Fatal("a size that is not whole sectors accepted")
	}
}

func TestWriteChangesOnlyDifferingSectors(t *testing.T) {
	orig := image(64 * sectorSize)
	d := tempDevice(t, orig)
	want := bytes.Clone(orig)
	want[10] ^= 1                // sector 0
	want[sectorSize+5] ^= 1      // sector 1, contiguous with 0
	want[40*sectorSize+511] ^= 1 // sector 40
	want[len(want)-1] ^= 1       // last sector
	rep, err := d.Write(orig, want, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Sectors != 4 || rep.Runs != 3 {
		t.Fatalf("report %+v", rep)
	}
	got, err := d.Read()
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("partition does not hold the new data (%v)", err)
	}
	// Nothing to do: no write at all.
	rep, err = d.Write(want, want, nil)
	if err != nil || rep.Sectors != 0 {
		t.Fatalf("no-op write: %+v %v", rep, err)
	}
}

func TestWriteRefusesChangedPartition(t *testing.T) {
	orig := image(8 * sectorSize)
	d := tempDevice(t, orig)
	stale := bytes.Clone(orig)
	stale[0] ^= 0xff // what we think is there is not
	want := bytes.Clone(orig)
	want[100] = 7
	if _, err := d.Write(stale, want, nil); !errors.Is(err, ErrChanged) {
		t.Fatalf("stale original: %v", err)
	}
	if got, _ := d.Read(); !bytes.Equal(got, orig) {
		t.Fatal("partition changed after a refused write")
	}
	if _, err := d.Write(orig, want[:len(want)-1], nil); err == nil {
		t.Fatal("a shorter image accepted")
	}
}

func meta(base string) Meta {
	return Meta{BaseSHA256: base, Firmware: "20260915", Board: "RGdsplus",
		Values: charger.Values{ChargeCurrent: 2000, InputCurrent: 1500, ChargeVoltage: 4400}}
}

func TestSaveListVerify(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "backups") // created by Save
	data := image(16 * sectorSize)
	now := time.Date(2026, 10, 1, 12, 30, 45, 0, time.Local)
	var last float64
	b, err := Save(dir, data, meta("base-a"), now, func(f float64) { last = f })
	if err != nil {
		t.Fatal(err)
	}
	if last != 1 {
		t.Fatalf("progress ended at %v", last)
	}
	if !strings.HasPrefix(filepath.Base(b.Image), "boot-20261001-123045-") || b.Meta.Size != int64(len(data)) {
		t.Fatalf("backup %+v", b)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".part") || strings.Contains(e.Name(), ".tmp") {
			t.Fatalf("leftover %s", e.Name())
		}
	}
	// A second backup in the same second gets its own name.
	b2, err := Save(dir, image(16*sectorSize), meta("base-b"), now, nil)
	if err != nil || b2.Image == b.Image {
		t.Fatalf("second backup %v %v", b2.Image, err)
	}

	list, problems := List([]string{dir, dir, filepath.Join(t.TempDir(), "missing")})
	if len(list) != 2 || len(problems) != 0 {
		t.Fatalf("list %+v, problems %v", list, problems)
	}
	var v Verifier
	if err := v.Verify(b); err != nil {
		t.Fatal(err)
	}
	if got, ok := v.Covering(list, "base-a"); !ok || got.Image != b.Image {
		t.Fatalf("covering base-a: %+v %v", got, ok)
	}
	if _, ok := v.Covering(list, "base-c"); ok {
		t.Fatal("covered an unknown firmware")
	}
	read, err := b.Read()
	if err != nil || !bytes.Equal(read, data) {
		t.Fatalf("read back: %v", err)
	}

	// Damage the file: it no longer verifies (the cache is keyed by mtime).
	time.Sleep(10 * time.Millisecond)
	f, _ := os.OpenFile(b.Image, os.O_WRONLY, 0)
	f.WriteAt([]byte{0xee}, 5)
	f.Close()
	os.Chtimes(b.Image, time.Now(), time.Now().Add(time.Second))
	if err := v.Verify(b); !errors.Is(err, ErrBackupDamaged) {
		t.Fatalf("damaged backup: %v", err)
	}
	if _, ok := v.Covering(list, "base-a"); ok {
		t.Fatal("a damaged backup still covers its firmware")
	}
	if _, err := b.Read(); !errors.Is(err, ErrBackupDamaged) {
		t.Fatalf("read of damaged backup: %v", err)
	}

	// A description without its image is not listed; a broken one is reported.
	os.Remove(b2.Image)
	os.WriteFile(filepath.Join(dir, "boot-broken.json"), []byte("{"), 0o644)
	os.WriteFile(filepath.Join(dir, "boot-broken.img"), data, 0o644)
	list, problems = List([]string{dir})
	if len(list) != 1 || len(problems) != 1 {
		t.Fatalf("after removal: %d backups, problems %v", len(list), problems)
	}
}

func TestDiffRuns(t *testing.T) {
	a := make([]byte, 5*sectorSize)
	b := bytes.Clone(a)
	b[0], b[sectorSize], b[3*sectorSize] = 1, 1, 1
	runs := diffRuns(a, b)
	if len(runs) != 2 || runs[0] != (run{0, 2 * sectorSize}) || runs[1] != (run{3 * sectorSize, sectorSize}) {
		t.Fatalf("runs %+v", runs)
	}
}

func flipByte(t *testing.T, path string, off int64) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b := make([]byte, 1)
	f.ReadAt(b, off)
	b[0] ^= 0xff
	if _, err := f.WriteAt(b, off); err != nil {
		t.Fatal(err)
	}
}

func noLeftovers(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		t.Errorf("left behind: %s", e.Name())
	}
}

func TestSaveRefusesWithoutSpace(t *testing.T) {
	old := checkSpace
	checkSpace = func(string, int64, int64) error { return &platform.NoSpaceError{Need: 65 << 20, Free: 10 << 20} }
	t.Cleanup(func() { checkSpace = old })
	dir := t.TempDir()
	var noSpace *platform.NoSpaceError
	if _, err := Save(dir, image(16*sectorSize), meta("base"), time.Now(), nil); !errors.As(err, &noSpace) {
		t.Fatalf("Save = %v", err)
	}
	noLeftovers(t, dir)
}

// The card returns other bytes than were written: no backup is kept.
func TestSaveRefusesBackupThatReadsBackDifferently(t *testing.T) {
	afterSync = func(path string) { flipByte(t, path, 100) }
	t.Cleanup(func() { afterSync = nil })
	dir := t.TempDir()
	if _, err := Save(dir, image(16*sectorSize), meta("base"), time.Now(), nil); err == nil || !strings.Contains(err.Error(), "reads back differently") {
		t.Fatalf("Save = %v", err)
	}
	noLeftovers(t, dir)
}

// A write that does not read back is written once more.
func TestWriteRetriesOnce(t *testing.T) {
	orig := image(16 * sectorSize)
	d := tempDevice(t, orig)
	want := bytes.Clone(orig)
	want[3*sectorSize] ^= 1
	afterWrite = func(path string, attempt int) {
		if attempt == 1 {
			flipByte(t, path, 3*sectorSize+1) // the card dropped a byte
		}
	}
	t.Cleanup(func() { afterWrite = nil })
	if _, err := d.Write(orig, want, nil); err != nil {
		t.Fatalf("retry did not fix the write: %v", err)
	}
	if got, _ := d.Read(); !bytes.Equal(got, want) {
		t.Fatal("partition does not hold the new data after the retry")
	}
}

// Every attempt reads back wrong: the previous content is put back and
// checked, and the error says so.
func TestWriteReportsVerifyFailure(t *testing.T) {
	orig := image(16 * sectorSize)
	d := tempDevice(t, orig)
	want := bytes.Clone(orig)
	want[5*sectorSize] ^= 1
	attempts := 0
	afterWrite = func(path string, attempt int) {
		if attempt == attemptRestore {
			return
		}
		attempts = attempt
		flipByte(t, path, 9*sectorSize) // every write of the new data reads back wrong
	}
	t.Cleanup(func() { afterWrite = nil })
	_, err := d.Write(orig, want, nil)
	var we *WriteError
	if !errors.Is(err, ErrVerify) || !errors.As(err, &we) || !we.Restored {
		t.Fatalf("Write = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("%d attempts, want 2", attempts)
	}
	if got, _ := d.Read(); !bytes.Equal(got, orig) {
		t.Fatal("the previous content is not back")
	}
}

func TestWriteReportsFailedRestore(t *testing.T) {
	orig := image(16 * sectorSize)
	d := tempDevice(t, orig)
	want := bytes.Clone(orig)
	want[5*sectorSize] ^= 1
	afterWrite = func(path string, attempt int) { flipByte(t, path, 9*sectorSize) } // the card is failing
	t.Cleanup(func() { afterWrite = nil })
	_, err := d.Write(orig, want, nil)
	var we *WriteError
	if !errors.As(err, &we) || we.Restored || !errors.Is(err, ErrVerify) {
		t.Fatalf("Write = %v", err)
	}
}

// A write that cannot even open the partition changes nothing, and the
// error says the previous content is in place.
func TestWriteIOErrorKeepsContent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	orig := image(8 * sectorSize)
	d := tempDevice(t, orig)
	if err := os.Chmod(d.Path, 0o444); err != nil {
		t.Fatal(err)
	}
	want := bytes.Clone(orig)
	want[0] ^= 1
	_, err := d.Write(orig, want, nil)
	var we *WriteError
	if !errors.As(err, &we) || !we.Restored || errors.Is(err, ErrVerify) {
		t.Fatalf("Write = %v", err)
	}
	if got, _ := d.Read(); !bytes.Equal(got, orig) {
		t.Fatal("the partition changed")
	}
}
