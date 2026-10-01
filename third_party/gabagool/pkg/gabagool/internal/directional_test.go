package internal

import (
	"testing"
	"time"

	"github.com/BrandonKowalski/gabagool/v2/pkg/gabagool/constants"
)

// A tap held a little longer than a frame must not repeat (DSFetch fork:
// the old 150 ms delay turned ordinary taps into double moves).
func TestNoRepeatWithinDelay(t *testing.T) {
	d := NewDirectionalInputWithTiming(120*time.Millisecond, 40*time.Millisecond)
	time.Sleep(50 * time.Millisecond) // idle frames before the press
	d.SetHeld(constants.VirtualButtonDown, true)
	time.Sleep(80 * time.Millisecond)
	if dir := d.Update(); dir != DirectionNone {
		t.Fatalf("repeated after 80 ms with a 120 ms delay: %v", dir)
	}
	time.Sleep(60 * time.Millisecond)
	if dir := d.Update(); dir != DirectionDown {
		t.Fatalf("no repeat after 140 ms: %v", dir)
	}
	time.Sleep(10 * time.Millisecond)
	if dir := d.Update(); dir != DirectionNone {
		t.Fatalf("second repeat came before the interval: %v", dir)
	}
	d.SetHeld(constants.VirtualButtonDown, false)
	if dir := d.Update(); dir != DirectionNone {
		t.Fatalf("repeat after release: %v", dir)
	}
}

func TestDefaultDelayIsLongerThanATap(t *testing.T) {
	if RepeatDelay < 300*time.Millisecond {
		t.Fatalf("RepeatDelay = %v; taps of 150-250 ms would double-move", RepeatDelay)
	}
}
