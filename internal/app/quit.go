package app

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	gaba "github.com/BrandonKowalski/gabagool/v2/pkg/gabagool"
	"github.com/BrandonKowalski/gabagool/v2/pkg/gabagool/constants"
	"github.com/veandco/go-sdl2/sdl"
)

// quitRequested is set when SDL queues a quit event: window close on the Mac,
// SIGTERM/SIGINT on the device (SDL turns those into SDL_QUIT). gabagool
// components consume the event and simply return, so screens check this flag
// to know the app must exit.
var quitRequested atomic.Bool

type quitWatcher struct{}

func (quitWatcher) FilterEvent(e sdl.Event, _ interface{}) bool {
	noteInput(e)
	if _, ok := e.(*sdl.QuitEvent); ok {
		quitRequested.Store(true)
		repeatOnce.Do(func() { go repeatQuit() })
	}
	return true
}

var (
	repeatOnce sync.Once
	repeatStop = make(chan struct{})
)

// repeatQuit keeps posting quit events: gabagool screens nest (the keyboard
// opens from a form) and each one consumes a single quit event, so without
// this the outer screen stays open.
func repeatQuit() {
	t := time.NewTicker(150 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-repeatStop:
			return
		case <-t.C:
			sdl.PushEvent(&sdl.QuitEvent{Type: sdl.QUIT, Timestamp: sdl.GetTicks()})
		}
	}
}

// stopQuitRepeat must run before SDL shuts down.
func stopQuitRepeat() {
	select {
	case <-repeatStop:
	default:
		close(repeatStop)
	}
}

func installQuitWatch() {
	sdl.AddEventWatch(quitWatcher{}, nil)
}

// QuitRequested reports whether the app should exit.
func QuitRequested() bool { return quitRequested.Load() }

// installQuitChord makes Select+Start leave the app from any screen. The
// SDL quit event makes the current gabagool component return; screens then
// see QuitRequested (a write in progress still runs to its end first).
func installQuitChord() {
	err := gaba.RegisterChord("quit", []constants.VirtualButton{
		constants.VirtualButtonSelect, constants.VirtualButtonStart,
	}, gaba.ChordOptions{Window: 500 * time.Millisecond, OnTrigger: func() {
		slog.Info("quit chord pressed")
		sdl.PushEvent(&sdl.QuitEvent{Type: sdl.QUIT, Timestamp: sdl.GetTicks()})
	}})
	if err != nil {
		slog.Warn("quit chord", "err", err)
	}
}
