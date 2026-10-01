package platform

import (
	"context"
	"encoding/binary"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Polling for the sleep watcher: the hall sensor is a sysfs attribute,
// cheap to read. The lid must stay shut for lidGrace with nothing holding
// the console awake before it is suspended, which also bridges the short
// gaps between the steps of a write (backup, boot partition, check).
// timerUnit scales SleepTimerSeconds. Variables so tests can shorten them.
var (
	lidPoll   = 500 * time.Millisecond
	lidGrace  = 3 * time.Second
	timerUnit = time.Second
)

// CanSleepOnLid reports whether the device has a lid sensor and a command
// to suspend it.
func (p *Platform) CanSleepOnLid() bool {
	return p.LidFile != "" && p.SleepCmd != ""
}

// CanSleepWhenIdle reports whether the system sleep timer can be read and
// the console suspended.
func (p *Platform) CanSleepWhenIdle() bool {
	return p.SleepTimerFile != "" && len(p.SleepTimerSeconds) > 0 && p.SleepCmd != ""
}

// SetLidSleep turns suspending on lid close on or off (a user setting).
func (p *Platform) SetLidSleep(on bool) { p.lidSleep.Store(on) }

// SetIdleSleep turns suspending after the system sleep timer on or off (a
// user setting).
func (p *Platform) SetIdleSleep(on bool) { p.idleSleep.Store(on) }

// Touch records user activity (a button press); the idle timer counts from
// the last one, or from the end of the last work (InhibitSleep).
func (p *Platform) Touch() { p.lastActive.Store(time.Now().UnixNano()) }

func (p *Platform) idleFor(now time.Time) time.Duration {
	return now.Sub(time.Unix(0, p.lastActive.Load()))
}

// LidClosed reads the lid sensor; ok is false when there is none.
func (p *Platform) LidClosed() (closed, ok bool) {
	if p.LidFile == "" {
		return false, false
	}
	data, err := os.ReadFile(p.FSPath(p.LidFile))
	if err != nil {
		return false, false
	}
	return strings.TrimSpace(string(data)) == p.LidClosedValue, true
}

// SleepTimer reads the system sleep timer: a little-endian int32 at
// SleepTimerOffset of SleepTimerFile (the stock menu's settings file) that
// indexes SleepTimerSeconds, 0 meaning never. The file's layout is not
// documented, so a size other than SleepTimerFileSize or a value out of
// range means unknown (ok false), as does a missing file.
func (p *Platform) SleepTimer() (d time.Duration, ok bool) {
	if p.SleepTimerFile == "" || len(p.SleepTimerSeconds) == 0 {
		return 0, false
	}
	data, err := os.ReadFile(p.FSPath(p.SleepTimerFile))
	if err != nil || (p.SleepTimerFileSize > 0 && len(data) != p.SleepTimerFileSize) ||
		p.SleepTimerOffset < 0 || p.SleepTimerOffset+4 > len(data) {
		return 0, false
	}
	i := int32(binary.LittleEndian.Uint32(data[p.SleepTimerOffset:]))
	if i < 0 || int(i) >= len(p.SleepTimerSeconds) {
		return 0, false
	}
	return time.Duration(p.SleepTimerSeconds[i]) * timerUnit, true
}

func (p *Platform) holdsActive() bool {
	p.sleepMu.Lock()
	defer p.sleepMu.Unlock()
	return p.sleepHolds > 0
}

// WatchSleep suspends the console the way the system menu does, which it
// does not do while an app runs:
//   - when the lid is closed, after lidGrace. After a suspend the lid has to
//     open before the next one, so waking the console with the power button
//     while the lid stays shut keeps it awake;
//   - when nothing was pressed for the system sleep timer (read once here:
//     it can only change in the system menu).
//
// Neither happens while reading or writing the boot partition holds the
// console awake (InhibitSleep); the idle time counts from the end of that
// work, and a write finishing with the lid shut ends in sleep. It returns
// when ctx is done, or at once on devices that support neither.
func (p *Platform) WatchSleep(ctx context.Context) {
	_, hasLid := p.LidClosed()
	hasLid = hasLid && p.CanSleepOnLid()
	timer, ok := p.SleepTimer()
	hasTimer := ok && timer > 0 && p.CanSleepWhenIdle()
	slog.Info("sleep watcher", "lid", hasLid, "system_timer", timer, "timer_known", ok)
	if !hasLid && !hasTimer {
		return
	}
	p.Touch()
	armed := true
	var closedSince time.Time
	t := time.NewTicker(lidPoll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if p.holdsActive() {
				closedSince = time.Time{}
				continue
			}
			reason := ""
			if hasLid {
				switch closed, ok := p.LidClosed(); {
				case !ok:
				case !closed:
					armed, closedSince = true, time.Time{}
				case !armed || !p.lidSleep.Load():
					closedSince = time.Time{}
				case closedSince.IsZero():
					closedSince = now
				case now.Sub(closedSince) >= lidGrace:
					reason = "lid closed"
					armed, closedSince = false, time.Time{}
				}
			}
			if reason == "" && hasTimer && p.idleSleep.Load() && p.idleFor(now) >= timer {
				reason = "idle"
			}
			if reason != "" {
				p.suspend(ctx, reason)
			}
		}
	}
}

// suspend runs the sleep command, which returns after the console wakes up.
func (p *Platform) suspend(ctx context.Context, reason string) {
	slog.Info("suspending", "reason", reason)
	out, err := exec.CommandContext(ctx, "/bin/sh", "-c", p.SleepCmd).CombinedOutput()
	if err != nil {
		slog.Warn("sleep command failed", "cmd", p.SleepCmd, "err", err, "out", string(out))
	}
	p.Touch() // the idle timer starts over after waking up
	slog.Info("resumed")
}
