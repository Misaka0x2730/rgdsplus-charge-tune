package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := LoadSettings(dir)
	if err != nil || s != DefaultSettings() {
		t.Fatalf("fresh: %+v %v", s, err)
	}
	s.BackupDir = "/mnt/mmc/rgdsplus-charge-tune/backups"
	s.LidSleep = false
	s.Language = "ru"
	if err := SaveSettings(dir, s); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings(dir)
	if err != nil || got != s {
		t.Fatalf("loaded %+v %v", got, err)
	}
	// A file from an older version keeps defaults for missing fields.
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"language":"de"}`), 0o644)
	got, _ = LoadSettings(dir)
	if got.Language != "de" || !got.LidSleep || !got.IdleSleep || got.BackupDir != "" {
		t.Fatalf("partial file: %+v", got)
	}
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{`), 0o644)
	if got, err := LoadSettings(dir); err == nil || got != DefaultSettings() {
		t.Fatalf("damaged file: %+v %v", got, err)
	}
}
