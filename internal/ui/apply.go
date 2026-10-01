package ui

import (
	"errors"
	"log/slog"
	"path/filepath"
	"strings"

	"rgdsplus-charge-tune/internal/charger"
	"rgdsplus-charge-tune/internal/flash"
	"rgdsplus-charge-tune/internal/tune"
)

// applyMode asks for confirmation, writes the mode and offers a reboot
// when the running kernel does not use the new values yet.
func (e *Env) applyMode(m charger.Mode) {
	st := e.state
	name := modeName(m)
	if st.bootErr != nil {
		info(errorMessage(T("err_cannot_change"), st.bootErr))
		return
	}
	if m.Matches(st.bootValues) {
		if st.runningErr == nil && m.Matches(st.running) {
			info(T("mode_active", name))
			return
		}
		e.askReboot(T("mode_pending", name))
		return
	}

	// The confirmation does not scroll: three paragraphs at most (the
	// values and the backup, what the mode does, the disclaimer).
	parts := []string{joinSentences(T("confirm_mode", name, m.ChargeCurrent, m.InputCurrent), T("confirm_backup", e.backupPlace()))}
	about := modeDescription(m)
	if m.Warning {
		about = joinSentences(about, T("confirm_warning"))
	}
	if about != "" {
		parts = append(parts, about)
	}
	parts = append(parts, T("disclaimer"))
	if !confirm(strings.Join(parts, "\n\n"), T("apply"), T("cancel")) {
		return
	}

	var res tune.Result
	err := e.work(T("applying"), func(p tune.Progress) error {
		var err error
		res, err = e.Engine.Apply(m, e.BackupDir(), p)
		return err
	})
	e.busy(T("reading_boot"), e.Refresh)
	if err != nil {
		slog.Error("apply mode", "mode", m.ID, "err", err)
		info(e.failureMessage(T("err_apply"), err, res.Backup.Image))
		return
	}
	msg := T("mode_written", name)
	if res.BackupCreated {
		msg += "\n\n" + T("backup_saved", e.placeOf(filepath.Dir(res.Backup.Image)))
	}
	if res.RebootNeeded {
		e.askReboot(msg + "\n\n" + T("reboot_needed"))
		return
	}
	info(msg)
}

// askReboot shows msg with the reboot question; on yes the app exits and
// the launcher reboots the console.
func (e *Env) askReboot(msg string) {
	if !confirm(msg+"\n\n"+T("reboot_now_q"), T("reboot"), T("later")) {
		return
	}
	if err := e.Platform.RequestReboot(); err != nil {
		slog.Error("reboot request", "err", err)
		info(errorMessage(T("err_reboot"), err))
		return
	}
	slog.Info("reboot requested")
	e.rebooting = true
}

// joinSentences puts two sentences in one paragraph: with a space, except
// after Chinese or Japanese punctuation, which needs none.
func joinSentences(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	case strings.HasSuffix(a, "。") || strings.HasSuffix(a, "！") || strings.HasSuffix(a, "？"):
		return a + b
	}
	return a + " " + b
}

// failureMessage explains a failed write. After a *flash.WriteError it says
// whether the previous content is back; if it is not, it names the backup
// to restore (backup, a file) and where the recovery instructions are.
func (e *Env) failureMessage(what string, err error, backup string) string {
	var we *flash.WriteError
	if !errors.As(err, &we) {
		return errorMessage(what, err)
	}
	if we.Restored {
		return what + "\n\n" + T("err_write_restored")
	}
	name, place := "?", "?"
	if backup != "" {
		name, place = filepath.Base(backup), e.placeOf(filepath.Dir(backup))
	}
	return what + "\n\n" + T("err_write_failed", name, place)
}
