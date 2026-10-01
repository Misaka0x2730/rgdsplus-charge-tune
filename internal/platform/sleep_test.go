package platform

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sleepRig is a platform with a fake hall sensor and a fake system settings
// file whose sleep command appends a line to a file.
type sleepRig struct {
	p      *Platform
	lid    string
	timer  string
	sleeps string
}

// newSleepRig starts WatchSleep with the lid open and the system timer at
// timerIndex (-1: no timer file). Timer values are milliseconds here.
func newSleepRig(t *testing.T, timerIndex int) *sleepRig {
	t.Helper()
	oldPoll, oldGrace, oldUnit := lidPoll, lidGrace, timerUnit
	lidPoll, lidGrace, timerUnit = 5*time.Millisecond, 40*time.Millisecond, time.Millisecond
	t.Cleanup(func() { lidPoll, lidGrace, timerUnit = oldPoll, oldGrace, oldUnit })

	p := testPlatform(t)
	r := &sleepRig{p: p, lid: filepath.Join(p.Root, "sys/hallkey"), timer: filepath.Join(p.Root, "data/attr.ini"),
		sleeps: filepath.Join(t.TempDir(), "sleeps")}
	os.MkdirAll(filepath.Dir(r.lid), 0o755)
	os.MkdirAll(filepath.Dir(r.timer), 0o755)
	r.setLid("3")
	p.LidFile, p.LidClosedValue = "/sys/hallkey", "2"
	p.SleepTimerFile, p.SleepTimerFileSize, p.SleepTimerOffset = "/data/attr.ini", 16, 8
	p.SleepTimerSeconds = []int{400, 0} // 400 ms, never
	if timerIndex >= 0 {
		r.setTimer(timerIndex, 16)
	}
	p.SleepCmd = "echo sleep >> '" + r.sleeps + "'"
	p.SetLidSleep(true)
	p.SetIdleSleep(true)

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { p.WatchSleep(ctx); close(stopped) }()
	t.Cleanup(func() { cancel(); <-stopped })
	return r
}

func (r *sleepRig) setLid(v string) { os.WriteFile(r.lid, []byte(v+"\n"), 0o644) }

func (r *sleepRig) setTimer(index, size int) {
	data := make([]byte, size)
	binary.LittleEndian.PutUint32(data[8:], uint32(index))
	os.WriteFile(r.timer, data, 0o644)
}

func (r *sleepRig) count() int {
	data, _ := os.ReadFile(r.sleeps)
	return strings.Count(string(data), "sleep")
}

// expect waits until the sleep count is want, then checks it stays there
// for settle.
func (r *sleepRig) expect(t *testing.T, want int, settle time.Duration) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for r.count() < want && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(settle)
	if got := r.count(); got != want {
		t.Fatalf("sleeps = %d, want %d", got, want)
	}
}

func TestLidClosedSuspendsOncePerClose(t *testing.T) {
	r := newSleepRig(t, -1)
	r.expect(t, 0, 4*lidGrace) // open
	r.setLid("2")
	r.expect(t, 1, 4*lidGrace) // closed: suspends once, not again while it stays shut
	r.setLid("3")
	time.Sleep(4 * lidPoll)
	r.setLid("2")
	r.expect(t, 2, 4*lidGrace) // reopened and closed again
}

func TestLidWaitsForWork(t *testing.T) {
	r := newSleepRig(t, -1)
	release := r.p.InhibitSleep() // a download in progress
	r.setLid("2")
	r.expect(t, 0, 4*lidGrace)
	release()
	r.expect(t, 1, 4*lidGrace) // the batch ended with the lid shut
}

func TestLidSleepSetting(t *testing.T) {
	r := newSleepRig(t, -1)
	r.p.SetLidSleep(false)
	r.setLid("2")
	r.expect(t, 0, 4*lidGrace)
	r.p.SetLidSleep(true)
	r.expect(t, 1, 4*lidGrace)
}

func TestIdleSleepAfterSystemTimer(t *testing.T) {
	r := newSleepRig(t, 0) // 400 ms
	// Presses keep it awake.
	for i := 0; i < 20; i++ {
		r.p.Touch()
		time.Sleep(30 * time.Millisecond)
	}
	if r.count() != 0 {
		t.Fatal("suspended despite presses")
	}
	r.expect(t, 1, 0) // then idle for the timer
}

func TestIdleTimerCountsFromEndOfWork(t *testing.T) {
	r := newSleepRig(t, 0)
	release := r.p.InhibitSleep()
	time.Sleep(600 * time.Millisecond) // a long download, longer than the timer
	if r.count() != 0 {
		t.Fatal("suspended while working")
	}
	release()
	time.Sleep(100 * time.Millisecond) // less than the timer after the work ended
	if r.count() != 0 {
		t.Fatal("suspended right after the work ended")
	}
	r.expect(t, 1, 0)
}

func TestIdleSleepOff(t *testing.T) {
	t.Run("never in the system settings", func(t *testing.T) {
		r := newSleepRig(t, 1)
		r.expect(t, 0, time.Second)
	})
	t.Run("off in DSFetch", func(t *testing.T) {
		r := newSleepRig(t, 0)
		r.p.SetIdleSleep(false)
		r.expect(t, 0, time.Second)
	})
}

func TestSleepTimerValidation(t *testing.T) {
	p := testPlatform(t)
	file := filepath.Join(p.Root, "data/attr.ini")
	os.MkdirAll(filepath.Dir(file), 0o755)
	p.SleepTimerFile, p.SleepTimerFileSize, p.SleepTimerOffset = "/data/attr.ini", 16, 8
	p.SleepTimerSeconds = []int{60, 120, 300, 600, 0}
	write := func(index uint32, size int) {
		data := make([]byte, size)
		binary.LittleEndian.PutUint32(data[8:], index)
		os.WriteFile(file, data, 0o644)
	}
	for _, c := range []struct {
		index uint32
		size  int
		want  time.Duration
		ok    bool
	}{
		{0, 16, time.Minute, true},
		{3, 16, 10 * time.Minute, true},
		{4, 16, 0, true},           // never
		{5, 16, 0, false},          // out of range
		{0xffffffff, 16, 0, false}, // negative
		{0, 20, 0, false},          // another firmware's layout
	} {
		write(c.index, c.size)
		if d, ok := p.SleepTimer(); d != c.want || ok != c.ok {
			t.Errorf("index %d size %d: %v, %v; want %v, %v", c.index, c.size, d, ok, c.want, c.ok)
		}
	}
}

func TestWatchSleepWithoutSensors(t *testing.T) {
	p := testPlatform(t)
	p.LidFile, p.LidClosedValue, p.SleepCmd = "/sys/none", "2", "true"
	p.SleepTimerFile, p.SleepTimerSeconds = "/data/none", []int{60}
	done := make(chan struct{})
	go func() { p.WatchSleep(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("WatchSleep kept running without a lid sensor or a timer")
	}
}
