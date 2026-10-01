package i18n

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/image/font/sfnt"
)

var (
	callRe = regexp.MustCompile(`\bT\("([a-z0-9_]+)"`)
	verbRe = regexp.MustCompile(`%[sdvq]`)
)

func catalog(t *testing.T, name string) map[string]string {
	t.Helper()
	data, err := locales.ReadFile("locales/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return parseSimpleTOML(data)
}

// catalogs returns every catalog by language code ("en", "zh-Hant", ...).
func catalogs(t *testing.T) map[string]map[string]string {
	t.Helper()
	entries, err := locales.ReadDir("locales")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]string{}
	for _, e := range entries {
		code := strings.TrimSuffix(strings.TrimPrefix(e.Name(), "active."), ".toml")
		out[code] = catalog(t, e.Name())
	}
	return out
}

// Every T("id") in the code must exist in every catalog with the same fmt
// verbs, so a missing translation never shows a raw id or garbles output.
func TestCatalogsCoverCode(t *testing.T) {
	used := map[string]string{}
	err := filepath.WalkDir("..", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range callRe.FindAllStringSubmatch(string(src), -1) {
			used[m[1]] = p
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(used) < 50 {
		t.Fatalf("only %d ids found; scanner broken?", len(used))
	}
	all := catalogs(t)
	en := all["en"]
	for code, cat := range all {
		for id, file := range used {
			if _, ok := cat[id]; !ok {
				t.Errorf("%s: %q missing in the %s catalog", file, id, code)
			}
		}
		for id, msg := range cat {
			want, ok := en[id]
			if !ok {
				t.Errorf("%s: %q is not in the English catalog", code, id)
			} else if strings.Join(verbRe.FindAllString(want, -1), "") != strings.Join(verbRe.FindAllString(msg, -1), "") {
				t.Errorf("%q: format verbs differ: en %q, %s %q", id, want, code, msg)
			}
		}
	}
	for id := range en {
		if _, ok := used[id]; !ok {
			t.Logf("unused message %q", id)
		}
	}
	for _, code := range Languages[1:] {
		if all[code] == nil {
			t.Errorf("offered language %q has no catalog", code)
		}
	}
	for code := range all {
		if !slices.Contains(Languages, code) {
			t.Errorf("catalog %q is not offered in Languages", code)
		}
	}
}

// Every offered language must render with the font gabagool embeds: a
// missing glyph shows as a box. (This is why zh-Hans and ko have no catalog.)
func TestOfferedLanguagesRender(t *testing.T) {
	data, err := os.ReadFile("../../third_party/gabagool/pkg/gabagool/internal/embedded_fonts/HackGenConsoleNF-Bold.ttf")
	if err != nil {
		t.Fatal(err)
	}
	font, err := sfnt.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	var buf sfnt.Buffer
	all := catalogs(t)
	for _, code := range Languages[1:] {
		missing := map[rune]bool{}
		for _, msg := range all[code] {
			for _, r := range msg + Name(code) {
				if r == '\n' {
					continue
				}
				if g, err := font.GlyphIndex(&buf, r); err != nil || g == 0 {
					missing[r] = true
				}
			}
		}
		for r := range missing {
			t.Errorf("%s: the font has no glyph for %q (U+%04X)", code, r, r)
		}
	}
}

func TestSwitchLanguage(t *testing.T) {
	if err := Init("ru"); err != nil {
		t.Fatal(err)
	}
	if got := T("err_nospace", 65, 10); got != "Не хватает места для копии: нужно 65 МБ, свободно 10 МБ." {
		t.Fatalf("ru: %q", got)
	}
	SetLanguage("en")
	if got := T("err_nospace", 65, 10); got != "Not enough free space for the backup: 65 MB needed, 10 MB free." {
		t.Fatalf("en: %q", got)
	}
	for lang, want := range map[string]string{
		"zh-Hant": "空間不足，無法備份：需要 65 MB，可用 10 MB。",
		"ja":      "バックアップの空き容量が足りません：65 MB 必要、空きは 10 MB です。",
		"pt":      "Sem espaço para a cópia: são necessários 65 MB, há 10 MB livres.",
	} {
		SetLanguage(lang)
		if got := T("err_nospace", 65, 10); got != want {
			t.Errorf("%s: %q, want %q", lang, got, want)
		}
	}
	SetLanguage("en")
	if got := T("no_such_id"); got != "no_such_id" {
		t.Fatalf("fallback: %q", got)
	}
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "ru_RU.UTF-8")
	if Resolve("auto") != "ru" {
		t.Fatal("auto did not pick Russian from LANG")
	}
}

func TestResolveLocale(t *testing.T) {
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
	for locale, want := range map[string]string{
		"de_DE.UTF-8": "de",
		"pt_BR.UTF-8": "pt",
		"es_MX":       "es",
		"fr_CA@euro":  "fr",
		"ja_JP.eucJP": "ja",
		"zh_TW.UTF-8": "zh-Hant",
		"zh_HK":       "zh-Hant",
		"zh-Hant-TW":  "zh-Hant",
		"zh_CN.UTF-8": "en", // no Simplified Chinese catalog
		"ko_KR.UTF-8": "en", // nor a Korean one
		"it_IT.UTF-8": "en",
		"C.UTF-8":     "en",
		"":            "en",
	} {
		t.Setenv("LC_ALL", locale)
		if got := Resolve("auto"); got != want {
			t.Errorf("LC_ALL=%q: auto = %q, want %q", locale, got, want)
		}
	}
	t.Setenv("LC_ALL", "C")
	t.Setenv("LANG", "fr_FR.UTF-8")
	if got := Resolve("auto"); got != "fr" {
		t.Errorf("LC_ALL=C, LANG=fr_FR: auto = %q, want fr", got)
	}
	if got := Resolve("ko"); got != "fr" {
		t.Errorf("a language that is not offered is not kept: %q, want auto's fr", got)
	}
}

// The stock firmware sets LANG to en_US even when the system is in Russian;
// the system setting must win.
func TestAutoPrefersSystemLanguage(t *testing.T) {
	defer SetSystemLanguage("")
	t.Setenv("LANG", "en_US.UTF-8")
	SetSystemLanguage("ru")
	if got := Resolve("auto"); got != "ru" {
		t.Fatalf("system ru: auto = %q", got)
	}
	if got := Resolve("en"); got != "en" {
		t.Fatalf("explicit en overridden: %q", got)
	}
	SetSystemLanguage("zh-Hant") // rgdsplus.json's code for index 1
	if got := Resolve("auto"); got != "zh-Hant" {
		t.Fatalf("system zh-Hant: auto = %q", got)
	}
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "ru_RU.UTF-8")
	SetSystemLanguage("ko") // no catalog
	if got := Resolve("auto"); got != "en" {
		t.Fatalf("system ko: auto = %q, want en", got)
	}
	SetSystemLanguage("")
	if got := Resolve("auto"); got != "ru" {
		t.Fatalf("no system language: auto = %q, want LANG's ru", got)
	}
}
