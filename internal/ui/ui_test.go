package ui

import (
	"fmt"
	"strings"
	"testing"

	"rgdsplus-charge-tune/internal/flash"
	"rgdsplus-charge-tune/internal/i18n"
	"rgdsplus-charge-tune/internal/platform"
)

func TestMain(m *testing.M) {
	if err := i18n.Init("en"); err != nil {
		panic(err)
	}
	m.Run()
}

// Mode names and descriptions are looked up by id, which the catalog test
// cannot see in the code: check them for every mode of the embedded
// configuration in every language.
func TestEveryModeIsTranslated(t *testing.T) {
	p, err := platform.Load(platform.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := i18n.Init("en"); err != nil {
		t.Fatal(err)
	}
	defer i18n.SetLanguage("en")
	for _, lang := range i18n.Languages[1:] {
		i18n.SetLanguage(lang)
		for _, m := range p.Modes {
			if modeName(m) == m.ID {
				t.Errorf("%s: no name for mode %s", lang, m.ID)
			}
			if modeDescription(m) == "" {
				t.Errorf("%s: no description for mode %s", lang, m.ID)
			}
		}
	}
}

func TestPadRight(t *testing.T) {
	if got := padRight("Ток", 6); got != "Ток   " {
		t.Fatalf("%q", got)
	}
	// Wide characters take two columns in the monospaced font.
	if got := padRight("充電", 6); got != "充電  " {
		t.Fatalf("%q", got)
	}
	if got := padRight("longer", 3); got != "longer " {
		t.Fatalf("%q", got)
	}
	if maxWidth("ab", "充電電流") != 8 {
		t.Fatal("maxWidth")
	}
	// The long vowel mark is wide although it is not in the Katakana script.
	if maxWidth("モード") != 6 {
		t.Fatalf("モード is %d columns", maxWidth("モード"))
	}
}

func TestJoinSentences(t *testing.T) {
	for _, c := range [][3]string{
		{"Fast.", "Backed up first.", "Fast. Backed up first."},
		{"快速。", "會先備份。", "快速。會先備份。"},
		{"", "b", "b"},
		{"a", "", "a"},
	} {
		if got := joinSentences(c[0], c[1]); got != c[2] {
			t.Errorf("joinSentences(%q, %q) = %q", c[0], c[1], got)
		}
	}
}

// The slash of every mode row sits under the slash of the column header, in
// every language.
func TestModeColumnsAlign(t *testing.T) {
	p, err := platform.Load(platform.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e := &Env{Platform: p}
	defer i18n.SetLanguage("en")
	col := func(s string) int {
		w := 0
		for _, r := range s[:strings.Index(s, " / ")] {
			w += runeWidth(r)
		}
		return w
	}
	for _, lang := range i18n.Languages[1:] {
		i18n.SetLanguage(lang)
		want := col(e.modeHeader())
		for _, m := range p.Modes {
			if got := col(e.modeRow(m)); got != want {
				t.Errorf("%s: %q has its slash at %d, the header at %d", lang, e.modeRow(m), got, want)
			}
		}
	}
}

func TestFailureMessage(t *testing.T) {
	p, err := platform.Load(platform.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e := &Env{Platform: p}
	backup := "/mnt/sdcard/rgdsplus-charge-tune/backups/boot-20261001-120000-eba3e65d.img"

	restored := e.failureMessage("Not written.", &flash.WriteError{Err: flash.ErrVerify, Restored: true}, backup)
	if !strings.Contains(restored, T("err_write_restored")) {
		t.Errorf("restored: %q", restored)
	}
	broken := e.failureMessage("Not written.", fmt.Errorf("apply: %w", &flash.WriteError{Err: flash.ErrVerify}), backup)
	for _, want := range []string{"boot-20261001-120000-eba3e65d.img", "TF2", "README.md"} {
		if !strings.Contains(broken, want) {
			t.Errorf("not restored: %q lacks %q", broken, want)
		}
	}
	plain := e.failureMessage("Not written.", flash.ErrChanged, backup)
	if plain != errorMessage("Not written.", flash.ErrChanged) {
		t.Errorf("other error: %q", plain)
	}
}
