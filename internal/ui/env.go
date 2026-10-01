// Package ui implements the screens. Each screen is a function that blocks
// on gabagool components and returns a Nav telling the router where to go.
package ui

import (
	"log/slog"

	gaba "github.com/BrandonKowalski/gabagool/v2/pkg/gabagool"
	"github.com/BrandonKowalski/gabagool/v2/pkg/gabagool/router"

	"rgdsplus-charge-tune/internal/charger"
	"rgdsplus-charge-tune/internal/i18n"
	"rgdsplus-charge-tune/internal/platform"
	"rgdsplus-charge-tune/internal/store"
	"rgdsplus-charge-tune/internal/tune"
)

// AppName is shown as the title and in About.
const AppName = "RG DS Plus Charge Tune"

// Screens.
const (
	ScreenMain router.Screen = iota
	ScreenRestore
	ScreenSettings
)

// Nav is what every screen returns: the next screen and its input.
type Nav struct {
	To    router.Screen
	Input any
}

// Exit ends the application.
var Exit = Nav{To: router.ScreenExit}

// Env holds dependencies and cross-screen state.
type Env struct {
	Platform *platform.Platform
	Engine   *tune.Engine
	Settings store.Settings
	DataDir  string
	// QuitRequested reports an SDL quit (window closed, SIGTERM).
	QuitRequested func() bool

	state       state
	mainSel     int
	mainVisited bool
	// rebooting: the user chose to reboot; every screen exits.
	rebooting bool
}

// state is what the main screen shows, refreshed after every write.
type state struct {
	running    charger.Values
	runningErr error
	// The boot partition: its values and base hash, or why it cannot be
	// used.
	bootValues charger.Values
	bootSHA256 string
	bootBase   string
	bootErr    error
	loaded     bool
}

// T is a shorthand for i18n.T.
func T(id string, args ...any) string { return i18n.T(id, args...) }

// ApplyLanguage translates the parts gabagool draws itself (the on-screen
// keyboard's footer and help).
func ApplyLanguage() {
	gaba.SetKeyboardLabels(gaba.KeyboardLabels{
		Delete:      T("kb_delete"),
		Space:       T("kb_space"),
		Symbols:     T("kb_symbols"),
		Shift:       T("kb_shift"),
		Cancel:      T("kb_cancel"),
		OK:          T("kb_ok"),
		HelpTitle:   T("kb_help_title"),
		HelpGeneral: helpLines("kb_help_move", "kb_help_type", "kb_help_delete", "kb_help_space", "kb_help_shift", "kb_help_cursor", "kb_help_cancel", "kb_help_ok"),
		HelpURL:     helpLines("kb_help_move", "kb_help_type", "kb_help_delete", "kb_help_symbols", "kb_help_shift", "kb_help_cursor", "kb_help_cancel", "kb_help_ok"),
		HelpNumeric: helpLines("kb_help_move", "kb_help_type", "kb_help_delete", "kb_help_cursor", "kb_help_cancel", "kb_help_ok"),
	})
}

func helpLines(ids ...string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = "• " + T(id)
	}
	return out
}

// SaveSettings persists and applies settings.
func (e *Env) SaveSettings(s store.Settings) {
	e.Settings = s
	e.Platform.SetLidSleep(s.LidSleep)
	e.Platform.SetIdleSleep(s.IdleSleep)
	i18n.SetLanguage(s.Language)
	ApplyLanguage()
	if err := store.SaveSettings(e.DataDir, s); err != nil {
		slog.Error("save settings", "err", err)
	}
}

// BackupDir is the backup folder to use (host path): the chosen one, or
// the first preset whose card is inserted.
func (e *Env) BackupDir() string {
	if e.Settings.BackupDir != "" {
		return e.Platform.FSPath(e.Settings.BackupDir)
	}
	return e.Platform.DefaultBackupDir()
}

// devicePath shows a host path the way the console sees it.
func (e *Env) devicePath(host string) string { return e.Platform.DevicePath(host) }

// Refresh reads the running values and the boot partition again.
func (e *Env) Refresh() {
	var st state
	st.running, st.runningErr = e.Platform.RunningValues()
	if st.runningErr != nil {
		slog.Warn("running values", "err", st.runningErr)
	}
	boot, err := e.Engine.ReadBoot()
	switch {
	case err != nil:
		st.bootErr = err
	case boot.Err != nil:
		st.bootErr = boot.Err
		st.bootSHA256, st.bootBase = boot.SHA256, boot.Base
	default:
		st.bootValues, st.bootSHA256, st.bootBase = boot.Values(), boot.SHA256, boot.Base
	}
	if st.bootErr != nil {
		slog.Warn("boot partition", "err", st.bootErr)
	} else {
		slog.Info("boot partition", "sha256", st.bootSHA256, "values", st.bootValues, "running", st.running)
	}
	st.loaded = true
	e.state = st
}

// modeName is the translated name of a mode (the id for unknown modes).
func modeName(m charger.Mode) string {
	id := "mode_" + m.ID // the catalogs name every mode of rgdsplus.json (see ui tests)
	if name := T(id); name != id {
		return name
	}
	return m.ID
}

// modeDescription is the translated description, "" if there is none.
func modeDescription(m charger.Mode) string {
	id := "mode_" + m.ID + "_desc"
	if d := T(id); d != id {
		return d
	}
	return ""
}

// valuesName names the mode with these values, or shows the values.
func (e *Env) valuesName(v charger.Values) string {
	if m, ok := charger.Find(e.Platform.Modes, v); ok {
		return modeName(m)
	}
	return T("custom_values", v.ChargeCurrent, v.InputCurrent)
}
