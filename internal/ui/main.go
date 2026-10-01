package ui

import (
	"fmt"

	gaba "github.com/BrandonKowalski/gabagool/v2/pkg/gabagool"
	"github.com/BrandonKowalski/gabagool/v2/pkg/gabagool/constants"

	"rgdsplus-charge-tune/internal/charger"
)

type rowKind int

const (
	rowMode rowKind = iota
	rowRestore
	rowSettings
)

type mainRow struct {
	kind rowKind
	mode charger.Mode
}

// Main is the start screen: the values the kernel booted with, the modes,
// the restore entry and the settings. A chooses, B quits.
func (e *Env) Main(any) (any, error) {
	if e.rebooting {
		return Exit, nil
	}
	if !e.state.loaded {
		e.busy(T("reading_boot"), e.Refresh)
	}
	st := e.state
	items := e.statusRows()

	items = append(items, gaba.MenuItem{Text: T("modes_header"), Separator: true},
		gaba.MenuItem{Text: e.modeHeader(), Info: true})
	firstMode := len(items)
	for _, m := range e.Platform.Modes {
		items = append(items, gaba.MenuItem{Text: e.modeRow(m), Metadata: mainRow{kind: rowMode, mode: m}})
	}
	items = append(items,
		gaba.MenuItem{Separator: true},
		gaba.MenuItem{Text: iconHistory + "  " + T("restore_backup"), Metadata: mainRow{kind: rowRestore}},
		gaba.MenuItem{Text: iconCog + "  " + T("settings"), Metadata: mainRow{kind: rowSettings}},
	)

	// First visit: start on the mode in use, which is what people compare.
	if !e.mainVisited {
		e.mainVisited = true
		e.mainSel = firstMode
		for i := firstMode; i < firstMode+len(e.Platform.Modes); i++ {
			if row := items[i].Metadata.(mainRow); st.runningErr == nil && row.mode.Matches(st.running) {
				e.mainSel = i
			}
		}
	}
	if e.mainSel >= len(items) {
		e.mainSel = len(items) - 1
	}

	opts := gaba.DefaultListOptions(AppName, items)
	// 47-pixel rows fit the values, the modes with their column header,
	// restore and settings on one screen (12 rows), so the values never
	// scroll away.
	opts.ItemHeight = 47
	opts.SelectedIndex = e.mainSel
	opts.HelpButton = constants.VirtualButtonMenu
	opts.HelpTitle = T("help_title")
	opts.HelpText = []string{T("help_main_mode"), T("help_main_restore"), T("help_main_b"), T("help_quit_anywhere")}
	opts.HelpExitText = T("help_exit")
	opts.FooterHelpItems = footer(btn("B", T("quit")), btn("A", T("select")))

	res, err := gaba.List(opts)
	if e.QuitRequested() || gaba.IsCancelled(err) {
		return Exit, nil
	}
	if err != nil {
		return nil, err
	}
	if len(res.Selected) == 0 {
		return Nav{To: ScreenMain}, nil
	}
	e.mainSel = res.Selected[0]
	row, ok := items[e.mainSel].Metadata.(mainRow)
	if !ok {
		return Nav{To: ScreenMain}, nil
	}
	switch row.kind {
	case rowMode:
		e.applyMode(row.mode)
		if e.rebooting || e.QuitRequested() {
			return Exit, nil
		}
	case rowRestore:
		return Nav{To: ScreenRestore}, nil
	case rowSettings:
		return Nav{To: ScreenSettings}, nil
	}
	return Nav{To: ScreenMain}, nil
}

// statusRows are the info lines: the values the kernel booted with, and
// what the boot partition will give after a reboot when that differs.
func (e *Env) statusRows() []gaba.MenuItem {
	st := e.state
	var out []gaba.MenuItem
	add := func(s string) { out = append(out, gaba.MenuItem{Text: s, Info: true}) }
	if st.runningErr != nil {
		add(T("info_running_unknown"))
	} else {
		labels := []string{T("info_voltage"), T("info_input"), T("info_charge")}
		w := maxWidth(labels...) + 3
		add(padRight(labels[0], w) + fmt.Sprintf("%.2f %s", float64(st.running.ChargeVoltage)/1000, T("unit_v")))
		add(padRight(labels[1], w) + fmt.Sprintf("%d %s", st.running.InputCurrent, T("unit_ma")))
		add(padRight(labels[2], w) + fmt.Sprintf("%d %s", st.running.ChargeCurrent, T("unit_ma")))
	}
	// A mode written but not booted yet is marked in its own row; values
	// that are no mode (changed by hand) get a line of their own.
	_, isMode := charger.Find(e.Platform.Modes, st.bootValues)
	switch {
	case st.bootErr != nil:
		add(T("info_boot_unusable"))
	case st.runningErr == nil && !st.bootValues.Same(st.running) && !isMode:
		add(iconBolt + " " + T("info_after_reboot", e.valuesName(st.bootValues), st.bootValues.ChargeCurrent, st.bootValues.InputCurrent))
	}
	return out
}

// Mode rows are a small table under a header naming the columns:
//
//	  Mode             charge / total (mA)
//	● Stock            2000   / 1500  · recommended
//
// The mark shows the mode in use; a mode written but not booted yet says so.
func (e *Env) modeColumns() (nameW, chargeW int) {
	names := []string{T("col_mode")}
	for _, m := range e.Platform.Modes {
		names = append(names, modeName(m))
	}
	return maxWidth(names...) + 2, max(4, maxWidth(T("col_charge"))) + 1
}

func (e *Env) modeHeader() string {
	nameW, chargeW := e.modeColumns()
	return "  " + padRight(T("col_mode"), nameW) + padRight(T("col_charge"), chargeW) + " / " +
		T("col_total") + " (" + T("unit_ma") + ")"
}

func (e *Env) modeRow(m charger.Mode) string {
	st := e.state
	mark := markNone
	if st.runningErr == nil && m.Matches(st.running) {
		mark = markActive
	}
	nameW, chargeW := e.modeColumns()
	text := fmt.Sprintf("%s %s%s / %d", mark, padRight(modeName(m), nameW),
		padRight(fmt.Sprint(m.ChargeCurrent), chargeW), m.InputCurrent)
	switch {
	case st.bootErr == nil && m.Matches(st.bootValues) && (st.runningErr != nil || !m.Matches(st.running)):
		text += "  " + iconBolt + " " + T("mode_after_reboot")
	case m.Recommended:
		text += "  · " + T("mode_recommended")
	}
	return text
}
