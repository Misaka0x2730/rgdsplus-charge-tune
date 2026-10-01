package flash

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"rgdsplus-charge-tune/internal/charger"
	"rgdsplus-charge-tune/internal/platform"
)

// Meta is the sidecar of a backup (<name>.json next to <name>.img).
type Meta struct {
	SHA256 string `json:"sha256"`
	// BaseSHA256 identifies the firmware: bootimg.Image.BaseSHA256, the
	// same for images that differ only in the charger mode.
	BaseSHA256 string         `json:"base_sha256"`
	Size       int64          `json:"size"`
	Created    time.Time      `json:"created"`
	Firmware   string         `json:"firmware,omitempty"` // version.ini at the time
	Board      string         `json:"board,omitempty"`
	Source     string         `json:"source,omitempty"` // the partition it was read from
	App        string         `json:"app,omitempty"`    // version of the app that saved it
	Values     charger.Values `json:"values"`
}

// Backup is a saved copy of the boot partition.
type Backup struct {
	Image string // path of the .img
	Meta  Meta
}

// Name is the file name without the extension.
func (b Backup) Name() string { return strings.TrimSuffix(filepath.Base(b.Image), ".img") }

const (
	backupPrefix = "boot-"
	chunk        = 1 << 20
)

// Test hooks: the free space check, and a call right after a backup file
// is synced, before it is read back.
var (
	checkSpace = platform.CheckSpace
	afterSync  func(path string)
)

// List reads the backups in dirs (missing ones are skipped; the same folder
// given twice is read once), newest first. Sidecars that cannot be read are
// reported in problems.
func List(dirs []string) (backups []Backup, problems []error) {
	seen := map[string]bool{}
	for _, dir := range dirs {
		abs, err := filepath.Abs(dir)
		if err != nil || seen[abs] {
			continue
		}
		seen[abs] = true
		matches, _ := filepath.Glob(filepath.Join(abs, backupPrefix+"*.json"))
		for _, js := range matches {
			img := strings.TrimSuffix(js, ".json") + ".img"
			if _, err := os.Stat(img); err != nil {
				continue // the sidecar of a deleted backup
			}
			data, err := os.ReadFile(js)
			if err != nil {
				problems = append(problems, err)
				continue
			}
			var m Meta
			if err := json.Unmarshal(data, &m); err != nil || m.SHA256 == "" || m.Size <= 0 {
				problems = append(problems, fmt.Errorf("%s: unreadable backup description", js))
				continue
			}
			backups = append(backups, Backup{Image: img, Meta: m})
		}
	}
	sort.SliceStable(backups, func(i, j int) bool { return backups[i].Meta.Created.After(backups[j].Meta.Created) })
	return backups, problems
}

// Verifier checks backup files against their sidecar and remembers the
// result per file (path, size, modification time), since hashing 64 MB
// takes a moment on the console.
type Verifier struct {
	mu    sync.Mutex
	cache map[string]error
}

// ErrBackupDamaged means a backup file does not match its description.
var ErrBackupDamaged = errors.New("the backup file does not match its description")

// Verify returns nil when the file has the recorded size and sha256.
func (v *Verifier) Verify(b Backup) error {
	st, err := os.Stat(b.Image)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%s|%d|%d", b.Image, st.Size(), st.ModTime().UnixNano())
	v.mu.Lock()
	if v.cache == nil {
		v.cache = map[string]error{}
	}
	if err, ok := v.cache[key]; ok {
		v.mu.Unlock()
		return err
	}
	v.mu.Unlock()

	err = verifyFile(b, st.Size())
	v.mu.Lock()
	v.cache[key] = err
	v.mu.Unlock()
	return err
}

func verifyFile(b Backup, size int64) error {
	if size != b.Meta.Size {
		return fmt.Errorf("%w: %d bytes instead of %d", ErrBackupDamaged, size, b.Meta.Size)
	}
	f, err := os.Open(b.Image)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != b.Meta.SHA256 {
		return fmt.Errorf("%w: sha256 differs", ErrBackupDamaged)
	}
	return nil
}

// Covering returns a verified backup of the same firmware (base hash).
func (v *Verifier) Covering(backups []Backup, base string) (Backup, bool) {
	for _, b := range backups {
		if b.Meta.BaseSHA256 == base && v.Verify(b) == nil {
			return b, true
		}
	}
	return Backup{}, false
}

// Read loads a backup's data and checks it against the sidecar again.
func (b Backup) Read() ([]byte, error) {
	data, err := os.ReadFile(b.Image)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != b.Meta.Size {
		return nil, ErrBackupDamaged
	}
	h := sha256.Sum256(data)
	if hex.EncodeToString(h[:]) != b.Meta.SHA256 {
		return nil, ErrBackupDamaged
	}
	return data, nil
}

// Save writes data as a new backup in dir. meta gives the description;
// SHA256, Size and Created are filled in here. The file is written as
// <name>.img.part, synced, read back from the card and compared, and only
// then renamed; the sidecar follows. On any error nothing but a removed
// .part is left. progress gets 0..1.
func Save(dir string, data []byte, meta Meta, now time.Time, progress func(float64)) (Backup, error) {
	if progress == nil {
		progress = func(float64) {}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Backup{}, err
	}
	sum := sha256.Sum256(data)
	meta.SHA256 = hex.EncodeToString(sum[:])
	meta.Size = int64(len(data))
	meta.Created = now.UTC()
	if err := checkSpace(dir, meta.Size+chunk, meta.Size); err != nil {
		return Backup{}, err
	}

	base := fmt.Sprintf("%s%s-%s", backupPrefix, now.Format("20060102-150405"), meta.SHA256[:8])
	img := filepath.Join(dir, base+".img")
	for i := 2; exists(img) || exists(img+".part"); i++ {
		img = filepath.Join(dir, fmt.Sprintf("%s-%d.img", base, i))
	}
	part := img + ".part"
	if err := writeVerified(part, data, sum, progress); err != nil {
		_ = os.Remove(part)
		return Backup{}, err
	}
	if err := os.Rename(part, img); err != nil {
		_ = os.Remove(part)
		return Backup{}, err
	}
	syncDir(dir)
	js, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return Backup{}, err
	}
	if err := writeFileAtomic(strings.TrimSuffix(img, ".img")+".json", append(js, '\n')); err != nil {
		// Without its description the image would not be listed: remove it
		// so that the next attempt starts clean.
		_ = os.Remove(img)
		return Backup{}, err
	}
	progress(1)
	return Backup{Image: img, Meta: meta}, nil
}

func writeVerified(path string, data []byte, sum [32]byte, progress func(float64)) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	for off := 0; off < len(data); off += chunk {
		end := min(off+chunk, len(data))
		if _, err := f.Write(data[off:end]); err != nil {
			f.Close()
			return err
		}
		progress(0.8 * float64(end) / float64(len(data)))
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	dropFileCache(f)
	if err := f.Close(); err != nil {
		return err
	}
	if afterSync != nil {
		afterSync(path)
	}
	r, err := os.Open(path)
	if err != nil {
		return err
	}
	defer r.Close()
	h := sha256.New()
	n, err := io.Copy(h, r)
	if err != nil {
		return err
	}
	var got [32]byte
	copy(got[:], h.Sum(nil))
	if n != int64(len(data)) || got != sum {
		return fmt.Errorf("the backup reads back differently from the card (%d of %d bytes)", n, len(data))
	}
	progress(0.95)
	return nil
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
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
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	syncDir(dir)
	return nil
}

// syncDir makes a rename durable where the filesystem supports it.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
