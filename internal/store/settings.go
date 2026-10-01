// Package store keeps the user's settings (data/settings.json).
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Settings are user preferences.
type Settings struct {
	Language string `json:"language"` // "auto" or a code from i18n.Languages
	// BackupDir is the backup folder as a device path; "" means automatic:
	// the first preset folder whose card is inserted.
	BackupDir string `json:"backup_dir"`
	LidSleep  bool   `json:"lid_sleep"`  // suspend when the lid is closed and nothing runs
	IdleSleep bool   `json:"idle_sleep"` // suspend after the system sleep timer when nothing runs
}

// DefaultSettings are used for a fresh install and for missing fields.
func DefaultSettings() Settings {
	return Settings{Language: "auto", LidSleep: true, IdleSleep: true}
}

// LoadSettings reads <dir>/settings.json, falling back to defaults.
func LoadSettings(dir string) (Settings, error) {
	s := DefaultSettings()
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return DefaultSettings(), err
	}
	return s, nil
}

// SaveSettings writes <dir>/settings.json.
func SaveSettings(dir string, s Settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, "settings.json"), append(data, '\n'), 0o644)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	// Some filesystems (FAT) ignore chmod; that is fine.
	_ = os.Chmod(path, perm)
	return nil
}
