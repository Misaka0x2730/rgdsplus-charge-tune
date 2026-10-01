// Package i18n translates UI strings through gabagool's go-i18n bundle,
// one catalog per language in locales/. Messages use fmt verbs for
// arguments.
package i18n

import (
	"embed"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

	gi18n "github.com/BrandonKowalski/gabagool/v2/pkg/gabagool/i18n"
)

//go:embed locales/*.toml
var locales embed.FS

// Languages offered in settings: "auto" (see Resolve), then the catalogs in
// the order of the console's own language list. Simplified Chinese (zh-Hans)
// and Korean (ko) have no catalog yet: the bundled font (HackGen) has no
// Hangul and lacks many simplified characters, so they would show as boxes.
// A console set to either gets English.
var Languages = []string{"auto", "zh-Hant", "en", "ja", "es", "ru", "de", "fr", "pt"}

// names are the languages' own names, for the language picker.
var names = map[string]string{
	"zh-Hant": "繁體中文",
	"en":      "English",
	"ja":      "日本語",
	"es":      "Español",
	"ru":      "Русский",
	"de":      "Deutsch",
	"fr":      "Français",
	"pt":      "Português",
}

const errorMarker = "I18N Error"

var (
	mu      sync.RWMutex
	current = "en"
	system  string // language of the device's own settings, "" = unknown
	english map[string]string
)

// SetSystemLanguage tells "auto" which language the device's own settings
// use ("ru", "zh-Hant", ...). It wins over $LANG, which the stock firmware
// only ever sets to English or Chinese. Languages not offered mean English.
func SetSystemLanguage(code string) {
	mu.Lock()
	system = strings.ToLower(strings.TrimSpace(code))
	mu.Unlock()
}

// Init loads the catalogs and selects a language ("auto" or one of
// Languages).
func Init(lang string) error {
	entries, err := locales.ReadDir("locales")
	if err != nil {
		return err
	}
	files := []gi18n.MessageFile{}
	for _, e := range entries {
		data, err := locales.ReadFile("locales/" + e.Name())
		if err != nil {
			return err
		}
		files = append(files, gi18n.MessageFile{Name: e.Name(), Content: data})
	}
	if err := gi18n.InitI18NFromBytes(files); err != nil {
		return err
	}
	if english == nil {
		data, err := locales.ReadFile("locales/active.en.toml")
		if err != nil {
			return err
		}
		english = parseSimpleTOML(data)
	}
	SetLanguage(lang)
	return nil
}

// Name returns a language's own name ("Deutsch" for "de").
func Name(code string) string {
	if n, ok := names[code]; ok {
		return n
	}
	return code
}

// SetLanguage switches the active language.
func SetLanguage(lang string) {
	resolved := Resolve(lang)
	mu.Lock()
	current = resolved
	mu.Unlock()
	_ = gi18n.SetWithCode(resolved)
}

// Resolve maps "auto" to a concrete language: the device's own setting
// when known (SetSystemLanguage), else the first of $LC_ALL / $LC_MESSAGES /
// $LANG in an offered language, else English.
func Resolve(lang string) string {
	if lang != "auto" && slices.Contains(Languages, lang) {
		return lang
	}
	mu.RLock()
	sys := system
	mu.RUnlock()
	if sys != "" {
		if m := match(sys); m != "" {
			return m
		}
		return "en"
	}
	for _, v := range []string{os.Getenv("LC_ALL"), os.Getenv("LC_MESSAGES"), os.Getenv("LANG")} {
		if m := match(v); m != "" {
			return m
		}
	}
	return "en"
}

// match maps a locale name ("pt_BR.UTF-8", "zh-TW", "de") to the offered
// language it is written in, or "" if there is none.
func match(locale string) string {
	l := strings.ToLower(strings.TrimSpace(locale))
	l, _, _ = strings.Cut(l, ".")
	l, _, _ = strings.Cut(l, "@")
	code, region, _ := strings.Cut(strings.ReplaceAll(l, "_", "-"), "-")
	if code == "zh" {
		code = "zh-Hans"
		if region == "tw" || region == "hk" || region == "mo" || strings.HasPrefix(region, "hant") {
			code = "zh-Hant"
		}
	}
	if code == "auto" || !slices.Contains(Languages, code) {
		return ""
	}
	return code
}

// Current returns the active language code.
func Current() string {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// T returns the translated message for id, formatted with args. Missing
// translations fall back to English, then to the id itself.
func T(id string, args ...any) string {
	msg := gi18n.GetString(id)
	if msg == errorMarker || msg == "" {
		if en, ok := english[id]; ok {
			msg = en
		} else {
			msg = id
		}
	}
	if len(args) > 0 {
		return fmt.Sprintf(msg, args...)
	}
	return msg
}

// parseSimpleTOML reads `key = "value"` lines (the catalog format we use),
// enough for the English fallback map.
func parseSimpleTOML(data []byte) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			v = strings.ReplaceAll(v[1:len(v)-1], `\n`, "\n")
			v = strings.ReplaceAll(v, `\"`, `"`)
		}
		out[k] = v
	}
	return out
}
