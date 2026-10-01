package ui

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	gaba "github.com/BrandonKowalski/gabagool/v2/pkg/gabagool"
	"github.com/BrandonKowalski/gabagool/v2/pkg/gabagool/constants"

	"rgdsplus-charge-tune/internal/tune"
)

// Restore lists the backups; A writes the chosen one back, B returns.
func (e *Env) Restore(any) (any, error) {
	var backups []tune.BackupInfo
	e.busy(T("checking_backups"), func() {
		backups = e.Engine.ListBackups(e.BackupDir(), e.state.bootBase)
	})
	if len(backups) == 0 {
		info(T("restore_empty"))
		return Nav{To: ScreenMain}, nil
	}
	items := make([]gaba.MenuItem, len(backups))
	for i, b := range backups {
		items[i] = gaba.MenuItem{Text: e.backupRow(b), Metadata: i}
	}
	opts := gaba.DefaultListOptions(T("restore_title"), items)
	opts.HelpButton = constants.VirtualButtonMenu
	opts.HelpTitle = T("help_title")
	opts.HelpText = []string{T("help_restore_a"), T("help_restore_b"), T("help_quit_anywhere")}
	opts.HelpExitText = T("help_exit")
	opts.FooterHelpItems = footer(btn("B", T("back")), btn("A", T("restore")))
	res, err := gaba.List(opts)
	if e.QuitRequested() {
		return Exit, nil
	}
	if gaba.IsCancelled(err) || len(res.Selected) == 0 {
		return Nav{To: ScreenMain}, nil
	}
	if err != nil {
		return nil, err
	}
	e.restore(backups[res.Selected[0]])
	if e.rebooting || e.QuitRequested() {
		return Exit, nil
	}
	return Nav{To: ScreenMain}, nil
}

func (e *Env) backupRow(b tune.BackupInfo) string {
	// Date and values fit a row; the firmware is in the confirmation.
	m := b.Meta
	parts := []string{m.Created.Local().Format("2006-01-02 15:04"),
		fmt.Sprintf("%d / %d %s", m.Values.ChargeCurrent, m.Values.InputCurrent, T("unit_ma"))}
	switch {
	case b.Err != nil:
		parts = append(parts, T("backup_damaged"))
	case b.Meta.SHA256 == e.state.bootSHA256:
		parts = append(parts, T("backup_current"))
	case !b.SameFirmware:
		parts = append(parts, T("backup_other_fw"))
	}
	return strings.Join(parts, " · ")
}

func (e *Env) restore(b tune.BackupInfo) {
	if b.Err != nil {
		info(errorMessage(T("err_restore"), b.Err))
		return
	}
	if b.Meta.SHA256 == e.state.bootSHA256 {
		info(T("backup_is_current"))
		return
	}
	when := b.Meta.Created.Local().Format("2006-01-02 15:04")
	if b.Meta.Firmware != "" {
		when += " (" + b.Meta.Firmware + ")"
	}
	msg := T("confirm_restore", when, b.Meta.Values.ChargeCurrent, b.Meta.Values.InputCurrent)
	if !b.SameFirmware {
		msg += "\n\n" + T("confirm_restore_other", b.Meta.Firmware)
	}
	msg += "\n\n" + T("disclaimer")
	if !confirm(msg, T("restore"), T("cancel")) {
		return
	}
	var res tune.Result
	err := e.work(T("restoring"), func(p tune.Progress) error {
		var err error
		res, err = e.Engine.Restore(b.Backup, e.BackupDir(), p)
		return err
	})
	e.busy(T("reading_boot"), e.Refresh)
	if err != nil {
		slog.Error("restore", "backup", b.Image, "err", err)
		// The backup being restored is known good: point to it.
		info(e.failureMessage(T("err_restore"), err, b.Image))
		return
	}
	msg = T("restore_done")
	if res.BackupCreated {
		msg += "\n\n" + T("backup_saved", e.placeOf(filepath.Dir(res.Backup.Image)))
	}
	if res.RebootNeeded {
		e.askReboot(msg + "\n\n" + T("reboot_needed"))
		return
	}
	info(msg)
}
