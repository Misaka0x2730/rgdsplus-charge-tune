package ui

import (
	"errors"
	"fmt"
	"go.uber.org/atomic"
	"strings"
	"time"

	gaba "github.com/BrandonKowalski/gabagool/v2/pkg/gabagool"
	"golang.org/x/text/width"

	"rgdsplus-charge-tune/internal/bootimg"
	"rgdsplus-charge-tune/internal/flash"
	"rgdsplus-charge-tune/internal/platform"
	"rgdsplus-charge-tune/internal/tune"
)

// Glyphs from the Nerd Font bundled with gabagool.
const (
	iconCog     = "" // nf-fa-cog
	iconHistory = "" // nf-fa-history
	iconBolt    = "" // nf-fa-bolt
	iconFolder  = "" // nf-fa-folder
	iconUp      = "" // nf-fa-arrow_up
	markActive  = "●"
	markNone    = " "
)

func footer(items ...gaba.FooterHelpItem) []gaba.FooterHelpItem { return items }

func btn(name, text string) gaba.FooterHelpItem {
	return gaba.FooterHelpItem{ButtonName: name, HelpText: text}
}

// startBtn labels the Start button in text: gabagool's play-triangle glyph
// was not recognisable as Start.
func startBtn(text string) gaba.FooterHelpItem {
	return gaba.FooterHelpItem{ButtonName: "START", HelpText: text}
}

// info shows a message until A or B is pressed.
func info(msg string) {
	_, _ = gaba.ConfirmationMessage(msg, footer(btn("A", T("ok"))), gaba.MessageOptions{})
}

// ReportSetAside says which damaged data files were renamed to .bad at
// start.
func ReportSetAside(names []string) {
	if len(names) > 0 {
		info(T("files_set_aside", strings.Join(names, "\n")))
	}
}

// confirm asks a yes/no question; A = yes.
func confirm(msg, yes, no string) bool {
	res, err := gaba.ConfirmationMessage(msg, footer(btn("B", no), btn("A", yes)), gaba.MessageOptions{})
	return err == nil && res != nil && res.Confirmed
}

// busy shows msg while fn runs, but only when fn takes longer than a
// moment, so quick work does not flash a screen.
func (e *Env) busy(msg string, fn func()) {
	release := e.Platform.InhibitSleep()
	defer release()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
		return
	case <-time.After(250 * time.Millisecond):
	}
	_, _ = gaba.ProcessMessage(msg, gaba.ProcessMessageOptions{}, func() (struct{}, error) {
		<-done
		return struct{}{}, nil
	})
	<-done // a quit event ends the message early; the work still finishes
}

// work runs a write with a progress bar. It cannot be cancelled: the
// partition must not be left half written, so even a quit request waits.
func (e *Env) work(msg string, fn func(progress tune.Progress) error) error {
	release := e.Platform.InhibitSleep()
	defer release()
	progress := atomic.NewFloat64(0)
	report := func(step tune.Step, f float64) {
		// read 0-10 %, backup 10-70 %, write 70-100 %.
		var v float64
		switch step {
		case tune.StepRead:
			v = 0.1 * f
		case tune.StepBackup:
			v = 0.1 + 0.6*f
		case tune.StepWrite:
			v = 0.7 + 0.3*f
		}
		if v > progress.Load() {
			progress.Store(v)
		}
	}
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		done <- fn(report)
		close(finished)
	}()
	_, _ = gaba.ProcessMessage(msg, gaba.ProcessMessageOptions{ShowProgressBar: true, Progress: progress}, func() (struct{}, error) {
		<-finished
		return struct{}{}, nil
	})
	return <-done // a quit event ends the message early; the write still finishes
}

// describe turns an error into a sentence for the user.
func describe(err error) string {
	var low *platform.LowBatteryError
	var dev *platform.UnsupportedDeviceError
	var noSpace *platform.NoSpaceError
	var unsupported *bootimg.UnsupportedError
	var mismatch *bootimg.HashMismatchError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &low):
		if low.Percent < 0 {
			return T("err_battery_unknown")
		}
		return T("err_low_battery", low.Percent, low.Min)
	case errors.As(err, &dev):
		return T("err_unsupported_device")
	case errors.Is(err, platform.ErrNotOnCard):
		return T("err_not_on_card")
	case errors.As(err, &noSpace):
		return T("err_nospace", mib(noSpace.Need), mib(noSpace.Free))
	case errors.Is(err, bootimg.ErrSigned):
		return T("err_signed")
	case errors.As(err, &mismatch):
		return T("err_hash", mismatch.Part)
	case errors.As(err, &unsupported), errors.Is(err, bootimg.ErrNotFIT), errors.Is(err, bootimg.ErrNoCharger),
		errors.Is(err, bootimg.ErrCopiesDiffer), errors.Is(err, bootimg.ErrMissingCopy):
		return T("err_unsupported_image")
	case errors.Is(err, flash.ErrChanged):
		return T("err_changed")
	case errors.Is(err, flash.ErrVerify):
		return T("err_verify")
	case errors.Is(err, flash.ErrBackupDamaged):
		return T("err_backup_damaged")
	}
	msg := err.Error()
	if len(msg) > 240 {
		msg = msg[:240] + "…"
	}
	return msg
}

// errorMessage formats "<what failed>\n\n<why>".
func errorMessage(what string, err error) string {
	return fmt.Sprintf("%s\n\n%s", what, describe(err))
}

func mib(n int64) int64 { return (n + 1<<20 - 1) >> 20 }

// padRight pads a label to a display width, counting wide (CJK) characters
// as two columns: the bundled font is monospaced.
func padRight(s string, cols int) string {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	if w >= cols {
		return s + " "
	}
	return s + strings.Repeat(" ", cols-w)
}

// runeWidth is the number of columns a character takes in the monospaced
// font: two for East Asian wide and fullwidth characters (including the
// katakana long vowel mark, which is not in the Katakana script), else one.
func runeWidth(r rune) int {
	switch width.LookupRune(r).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	}
	return 1
}

// maxWidth is the widest display width among labels.
func maxWidth(labels ...string) int {
	m := 0
	for _, l := range labels {
		w := 0
		for _, r := range l {
			w += runeWidth(r)
		}
		m = max(m, w)
	}
	return m
}
