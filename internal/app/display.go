package app

import (
	"log/slog"

	gaba "github.com/BrandonKowalski/gabagool/v2/pkg/gabagool"
	"github.com/veandco/go-sdl2/sdl"
)

// logDisplays records what SDL sees. On the dual-screen RG DS Plus this is
// the first thing to check in Stage 0 (which display is the top screen).
func logDisplays() {
	n, err := sdl.GetNumVideoDisplays()
	if err != nil {
		slog.Warn("SDL displays", "err", err)
		return
	}
	driver, _ := sdl.GetCurrentVideoDriver()
	slog.Info("SDL video", "driver", driver, "displays", n)
	for i := 0; i < n; i++ {
		name, _ := sdl.GetDisplayName(i)
		bounds, _ := sdl.GetDisplayBounds(i)
		mode, _ := sdl.GetCurrentDisplayMode(i)
		slog.Info("SDL display", "index", i, "name", name,
			"bounds", bounds, "mode_w", mode.W, "mode_h", mode.H, "refresh", mode.RefreshRate)
	}
}

// moveToDisplay puts the gabagool window on another display. gabagool always
// creates its window on display 0; this is a best-effort move that works on
// desktop drivers. Whether KMSDRM honours it is to be verified on the device.
func moveToDisplay(index int) {
	n, err := sdl.GetNumVideoDisplays()
	if err != nil || index < 0 || index >= n {
		slog.Warn("requested display not available", "screen", index, "displays", n)
		return
	}
	bounds, err := sdl.GetDisplayBounds(index)
	if err != nil {
		slog.Warn("display bounds", "screen", index, "err", err)
		return
	}
	w := gaba.GetWindow().Window
	w.SetPosition(bounds.X, bounds.Y)
	if idx, err := w.GetDisplayIndex(); err == nil {
		slog.Info("window moved", "requested", index, "now_on", idx)
	}
}

// placeWindow makes the UI fullscreen on the requested display (device
// only). The stock firmware runs Weston: a plain window would sit on the
// desktop shell, and Wayland clients cannot move windows between outputs.
// gabagool always creates its window at (0,0), i.e. on display 0, so for
// another display the window and renderer are recreated there, fullscreen
// from the start (sdlprobe showed this lands on the right output).
func placeWindow(screen int, rotated bool) {
	w := gaba.GetWindow()
	n, err := sdl.GetNumVideoDisplays()
	if err != nil || screen < 0 || screen >= n {
		screen = 0
	}
	cur, _ := w.Window.GetDisplayIndex()
	if screen == cur || rotated {
		if rotated && screen != cur {
			slog.Warn("cannot move a rotated window to another display", "screen", screen)
		}
		if err := w.Window.SetFullscreen(sdl.WINDOW_FULLSCREEN_DESKTOP); err != nil {
			slog.Warn("fullscreen", "err", err)
		}
		return
	}
	bounds, err := sdl.GetDisplayBounds(screen)
	if err != nil {
		slog.Warn("display bounds", "screen", screen, "err", err)
		return
	}
	win, err := sdl.CreateWindow(w.Title, bounds.X, bounds.Y, bounds.W, bounds.H,
		sdl.WINDOW_SHOWN|sdl.WINDOW_BORDERLESS|sdl.WINDOW_FULLSCREEN_DESKTOP)
	if err != nil {
		slog.Warn("create window on display", "screen", screen, "err", err)
		return
	}
	ren, err := sdl.CreateRenderer(win, -1, sdl.RENDERER_ACCELERATED|sdl.RENDERER_PRESENTVSYNC|sdl.RENDERER_TARGETTEXTURE)
	if err != nil {
		slog.Warn("create renderer on display", "screen", screen, "err", err)
		win.Destroy()
		return
	}
	// Same logical canvas as gabagool computed for display 0 (both panels
	// are 1024x768), so its layout code is unaffected.
	_ = ren.SetLogicalSize(w.GetWidth(), w.GetHeight())
	if w.Background != nil {
		w.Background.Destroy()
		w.Background = nil
	}
	w.Renderer.Destroy()
	w.Window.Destroy()
	w.Window, w.Renderer = win, ren
	for i := 0; i < 3; i++ {
		ren.SetDrawColor(0, 0, 0, 255)
		ren.Clear()
		ren.Present()
	}
	idx, _ := win.GetDisplayIndex()
	slog.Info("window placed", "requested", screen, "now_on", idx)
}
