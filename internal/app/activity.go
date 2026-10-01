package app

import "github.com/veandco/go-sdl2/sdl"

// onActivity is told about every button press, for the idle sleep timer.
// Set before the SDL event filter is installed.
var onActivity func()

// stickDeadzone ignores analog noise: only a clear push counts as activity,
// or a drifting stick would keep the console awake forever.
const stickDeadzone = 16000

// noteInput reports user input among the events SDL delivers.
func noteInput(e sdl.Event) {
	if onActivity == nil {
		return
	}
	active := false
	switch ev := e.(type) {
	case *sdl.KeyboardEvent:
		active = ev.Type == sdl.KEYDOWN
	case *sdl.JoyButtonEvent:
		active = ev.State == sdl.PRESSED
	case *sdl.ControllerButtonEvent:
		active = ev.State == sdl.PRESSED
	case *sdl.JoyHatEvent:
		active = ev.Value != sdl.HAT_CENTERED
	case *sdl.JoyAxisEvent:
		active = ev.Value > stickDeadzone || ev.Value < -stickDeadzone
	case *sdl.ControllerAxisEvent:
		active = ev.Value > stickDeadzone || ev.Value < -stickDeadzone
	case *sdl.MouseButtonEvent, *sdl.TouchFingerEvent:
		active = true
	}
	if active {
		onActivity()
	}
}
