package app

// Dev-only UI automation, used to smoke-test screens and take the README
// screenshots without a person at the keyboard. Enabled with -dev and
// CHARGETUNE_AUTOPILOT, e.g.
//
//	CHARGETUNE_AUTOPILOT="wait:800 shot:main.png down a wait:300 shot:confirm.png b" \
//	CHARGETUNE_SHOTS=build/shots task run
//
// Commands (space separated):
//
//	up down left right a b x y l1 r1 start select menu  press a button
//	type:<text>   type characters as key presses (keyboard screen)
//	wait:<ms>     sleep
//	shot:<file>   save a PNG of the current frame into $CHARGETUNE_SHOTS
//	quit          request application exit
//
// Other development aids (environment variables, all only with -dev):
//
//	CHARGETUNE_FAKE_WRITE_FAULT=verify    writes of the boot partition read
//	                                      back wrong; the old content is put
//	                                      back (the "nothing changed" screen)
//	CHARGETUNE_FAKE_WRITE_FAULT=all       putting it back fails too (the
//	                                      "restore the backup" screen)

import (
	"image"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	gaba "github.com/BrandonKowalski/gabagool/v2/pkg/gabagool"
	"github.com/BrandonKowalski/gabagool/v2/pkg/gabagool/constants"
	"github.com/veandco/go-sdl2/sdl"
)

var autopilotKeys = map[string]sdl.Keycode{
	"up": sdl.K_UP, "down": sdl.K_DOWN, "left": sdl.K_LEFT, "right": sdl.K_RIGHT,
	"a": sdl.K_a, "b": sdl.K_b, "x": sdl.K_x, "y": sdl.K_y,
	"l1": sdl.K_l, "r1": sdl.K_r, "start": sdl.K_RETURN, "select": sdl.K_SPACE, "menu": sdl.K_h,
}

var pendingShot atomic.Value // string: file to write on the next chord

// autopilotEnabled must be checked before gaba.Init: screenshots need the
// software renderer, whose window surface still holds the last frame.
func autopilotEnabled(dev bool) bool {
	return dev && os.Getenv("CHARGETUNE_AUTOPILOT") != ""
}

func prepareAutopilot() {
	os.Setenv("SDL_RENDER_DRIVER", "software")
}

func startAutopilot() {
	script := strings.Fields(os.Getenv("CHARGETUNE_AUTOPILOT"))
	dir := os.Getenv("CHARGETUNE_SHOTS")
	if dir == "" {
		dir = "build/shots"
	}
	_ = os.MkdirAll(dir, 0o755)

	// L2+R2 (';' and 't' in gabagool's default keyboard map) is the
	// screenshot chord; the callback runs on the main thread between frames.
	_ = gaba.RegisterChord("dev_screenshot", []constants.VirtualButton{
		constants.VirtualButtonL2, constants.VirtualButtonR2,
	}, gaba.ChordOptions{Window: 500 * time.Millisecond, OnTrigger: func() {
		name, _ := pendingShot.Load().(string)
		if name == "" {
			return
		}
		pendingShot.Store("")
		if err := saveScreenshot(filepath.Join(dir, name)); err != nil {
			slog.Error("screenshot", "err", err)
		} else {
			slog.Info("screenshot saved", "file", name)
		}
	}})

	go func() {
		time.Sleep(300 * time.Millisecond)
		for _, cmd := range script {
			runAutopilotCommand(cmd)
		}
		slog.Info("autopilot finished")
	}()
}

func runAutopilotCommand(cmd string) {
	name, arg, _ := strings.Cut(cmd, ":")
	switch name {
	case "wait":
		ms, _ := strconv.Atoi(arg)
		time.Sleep(time.Duration(ms) * time.Millisecond)
	case "shot":
		pendingShot.Store(arg)
		pushKey(sdl.K_SEMICOLON, true)
		pushKey(sdl.K_t, true)
		time.Sleep(150 * time.Millisecond)
		pushKey(sdl.K_SEMICOLON, false)
		pushKey(sdl.K_t, false)
		time.Sleep(150 * time.Millisecond)
	case "type":
		for _, r := range arg {
			k := sdl.Keycode(r)
			pushKey(k, true)
			time.Sleep(60 * time.Millisecond)
			pushKey(k, false)
			time.Sleep(60 * time.Millisecond)
		}
	case "quit":
		sdl.PushEvent(&sdl.QuitEvent{Type: sdl.QUIT, Timestamp: sdl.GetTicks()})
	default:
		k, ok := autopilotKeys[name]
		if !ok {
			slog.Warn("autopilot: unknown command", "cmd", cmd)
			return
		}
		pushKey(k, true)
		time.Sleep(80 * time.Millisecond)
		pushKey(k, false)
		time.Sleep(250 * time.Millisecond)
	}
}

func pushKey(k sdl.Keycode, down bool) {
	ev := &sdl.KeyboardEvent{
		Type:      sdl.KEYDOWN,
		Timestamp: sdl.GetTicks(),
		State:     sdl.PRESSED,
		Keysym:    sdl.Keysym{Sym: k},
	}
	if !down {
		ev.Type = sdl.KEYUP
		ev.State = sdl.RELEASED
	}
	if _, err := sdl.PushEvent(ev); err != nil {
		slog.Warn("autopilot push", "err", err)
	}
}

func saveScreenshot(path string) error {
	r := gaba.GetWindow().Renderer
	w, h, err := r.GetOutputSize()
	if err != nil {
		return err
	}
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	// ABGR8888 is R,G,B,A in memory on little-endian, i.e. image.RGBA layout.
	if err := r.ReadPixels(nil, sdl.PIXELFORMAT_ABGR8888, unsafe.Pointer(&img.Pix[0]), img.Stride); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
