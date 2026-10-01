// Package app wires dependencies together and runs the screen router.
package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	gaba "github.com/BrandonKowalski/gabagool/v2/pkg/gabagool"
	"github.com/BrandonKowalski/gabagool/v2/pkg/gabagool/router"

	"rgdsplus-charge-tune/internal/flash"
	"rgdsplus-charge-tune/internal/i18n"
	"rgdsplus-charge-tune/internal/platform"
	"rgdsplus-charge-tune/internal/store"
	"rgdsplus-charge-tune/internal/tune"
	"rgdsplus-charge-tune/internal/ui"
	"rgdsplus-charge-tune/internal/version"
)

// Options come from command-line flags.
type Options struct {
	Dev     bool
	Root    string
	DataDir string
	Screen  int
	Rotate  int
}

// Run initialises the UI and blocks until the user quits.
func Run(plat *platform.Platform, opts Options) error {
	settings, err := store.LoadSettings(opts.DataDir)
	if err != nil {
		slog.Warn("settings unreadable, using defaults", "err", err)
	}
	if lang := plat.SystemLanguage(); lang != "" {
		slog.Info("system language", "lang", lang)
		i18n.SetSystemLanguage(lang)
	}
	if err := i18n.Init(settings.Language); err != nil {
		return err
	}

	if opts.Dev {
		// CHARGETUNE_FAKE_WRITE_FAULT=verify|all shows the write error
		// screens (internal/app/devtools.go).
		flash.InjectFaults(os.Getenv("CHARGETUNE_FAKE_WRITE_FAULT"))
	}
	env := &ui.Env{
		Platform:      plat,
		Engine:        tune.New(plat, version.Version),
		Settings:      settings,
		DataDir:       opts.DataDir,
		QuitRequested: QuitRequested,
	}

	// Follow the system "swap screens" setting unless -screen was given.
	if opts.Screen < 0 && plat.ScreensSwapped() {
		slog.Info("screens swapped in system settings: using display 1")
		opts.Screen = 1
	}
	onActivity = plat.Touch // button presses reset the idle sleep timer
	initUI(opts)
	defer gaba.Close()
	defer stopQuitRepeat()
	ui.ApplyLanguage()

	// Suspend on lid close and after the system sleep timer, like the system
	// menu (not while writing).
	plat.SetLidSleep(settings.LidSleep)
	plat.SetIdleSleep(settings.IdleSleep)
	sleepCtx, stopSleep := context.WithCancel(context.Background())
	defer stopSleep()
	go plat.WatchSleep(sleepCtx)

	// Damaged data files were renamed to .bad at start: say so now that
	// there is a screen.
	ui.ReportSetAside(plat.SetAside)

	r := router.New()
	r.Register(ui.ScreenMain, env.Main)
	r.Register(ui.ScreenRestore, env.Restore)
	r.Register(ui.ScreenSettings, env.SettingsScreen)
	r.OnTransition(func(from router.Screen, result any, _ *router.Stack) (router.Screen, any) {
		if QuitRequested() {
			return router.ScreenExit, nil
		}
		nav, ok := result.(ui.Nav)
		if !ok {
			slog.Error("screen returned no navigation", "screen", from)
			return ui.ScreenMain, nil
		}
		return nav.To, nav.Input
	})
	return r.Run(ui.ScreenMain, nil)
}

func initUI(opts Options) {
	// Without focus SDL drops joystick events; a Wayland compositor may not
	// focus a freshly launched window, and this app is gamepad-only.
	if os.Getenv("SDL_JOYSTICK_ALLOW_BACKGROUND_EVENTS") == "" {
		os.Setenv("SDL_JOYSTICK_ALLOW_BACKGROUND_EVENTS", "1")
	}
	autopilot := autopilotEnabled(opts.Dev)
	if autopilot {
		prepareAutopilot()
	}
	if b, err := os.ReadFile(filepath.Join(opts.DataDir, "input_mapping.json")); err == nil {
		gaba.SetInputMappingBytes(b)
		slog.Info("custom input mapping loaded")
	}
	gaba.Init(gaba.Options{
		WindowTitle:        ui.AppName,
		LogPath:            filepath.Join(opts.DataDir, "gabagool.log"),
		DisplayOrientation: orientation(opts.Rotate),
	})
	installQuitWatch()
	installQuitChord()
	logDisplays()
	if !opts.Dev {
		placeWindow(opts.Screen, opts.Rotate != 0)
	} else if opts.Screen >= 0 {
		moveToDisplay(opts.Screen)
	}
	if autopilot {
		startAutopilot()
	}
}

func orientation(deg int) gaba.DisplayOrientation {
	switch deg {
	case 90:
		return gaba.OrientationRotate90
	case 180:
		return gaba.OrientationRotate180
	case 270:
		return gaba.OrientationRotate270
	default:
		return gaba.OrientationNormal
	}
}
