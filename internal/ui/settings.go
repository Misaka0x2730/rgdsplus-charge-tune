package ui

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	gaba "github.com/BrandonKowalski/gabagool/v2/pkg/gabagool"
	"github.com/BrandonKowalski/gabagool/v2/pkg/gabagool/constants"

	"rgdsplus-charge-tune/internal/i18n"
	"rgdsplus-charge-tune/internal/version"
)

func choiceOptions(values []string, labels []string, current string) ([]gaba.Option, int) {
	opts := make([]gaba.Option, len(values))
	sel := 0
	for i, v := range values {
		opts[i] = gaba.Option{DisplayName: labels[i], Value: v}
		if v == current {
			sel = i
		}
	}
	return opts, sel
}

func optValue(it gaba.ItemWithOptions) string {
	s, _ := it.Options[it.SelectedOption].Value.(string)
	return s
}

func yesNoOptions() []gaba.Option {
	return []gaba.Option{{DisplayName: T("no"), Value: false}, {DisplayName: T("yes"), Value: true}}
}

func boolIndex(b bool) int {
	if b {
		return 1
	}
	return 0
}

func optBool(it gaba.ItemWithOptions) bool {
	b, _ := it.Options[it.SelectedOption].Value.(bool)
	return b
}

// SettingsScreen edits preferences; B saves and goes back.
func (e *Env) SettingsScreen(any) (any, error) {
	s := e.Settings

	langNames := []string{T("lang_auto")}
	for _, code := range i18n.Languages[1:] {
		langNames = append(langNames, i18n.Name(code))
	}
	langOpts, langSel := choiceOptions(i18n.Languages, langNames, s.Language)

	const backupRow, langRow = 0, 1
	items := []gaba.ItemWithOptions{
		{Item: gaba.MenuItem{Text: T("s_backup_dir")}, Options: []gaba.Option{{DisplayName: e.backupDirLabel(s.BackupDir), Type: gaba.OptionTypeClickable}}},
		{Item: gaba.MenuItem{Text: T("s_language")}, Options: langOpts, SelectedOption: langSel},
	}
	lidRow := -1
	if e.Platform.CanSleepOnLid() {
		lidRow = len(items)
		items = append(items, gaba.ItemWithOptions{Item: gaba.MenuItem{Text: T("s_lid_sleep")}, Options: yesNoOptions(), SelectedOption: boolIndex(s.LidSleep)})
	}
	idleRow := -1
	if e.Platform.CanSleepWhenIdle() {
		idleRow = len(items)
		items = append(items, gaba.ItemWithOptions{Item: gaba.MenuItem{Text: T("s_idle_sleep")}, Options: yesNoOptions(), SelectedOption: boolIndex(s.IdleSleep)})
	}
	aboutRow := len(items)
	items = append(items, gaba.ItemWithOptions{Item: gaba.MenuItem{Text: T("about")}, Options: []gaba.Option{{Type: gaba.OptionTypeClickable}}})

	settings := gaba.OptionListSettings{
		FooterHelpItems:  footer(btn("B", T("save_back")), btn("A", T("change"))),
		ListPickerButton: constants.VirtualButtonA,
		HelpExitText:     T("help_exit"),
	}
	for {
		res, err := gaba.OptionsList(T("settings"), settings, items)
		if e.QuitRequested() {
			return Exit, nil
		}
		if err != nil && !gaba.IsCancelled(err) {
			return nil, err
		}
		if err == nil && res.Selected == aboutRow {
			settings.InitialSelectedIndex = aboutRow
			e.showAbout()
			continue
		}
		if err == nil && res.Selected == backupRow {
			settings.InitialSelectedIndex = backupRow
			if dir, ok := e.chooseBackupDir(s.BackupDir); ok {
				s.BackupDir = dir
				items[backupRow].Options[0].DisplayName = e.backupDirLabel(dir)
			}
			continue
		}
		break
	}

	// gabagool edits items in place, so the values are there even after B.
	s.Language = optValue(items[langRow])
	if lidRow >= 0 {
		s.LidSleep = optBool(items[lidRow])
	}
	if idleRow >= 0 {
		s.IdleSleep = optBool(items[idleRow])
	}
	e.SaveSettings(s)
	return Nav{To: ScreenMain}, nil
}

// backupDirLabel names a backup folder setting: "Automatic (TF2)", a
// preset's card, or the card of a folder chosen on it.
func (e *Env) backupDirLabel(dir string) string {
	if dir == "" {
		return T("backup_dir_auto", e.backupPlace())
	}
	for _, l := range e.Platform.BackupLocations {
		if l.Dir == dir {
			return l.Label
		}
	}
	for _, l := range e.Platform.BackupLocations {
		if strings.HasPrefix(dir, strings.TrimSuffix(l.Card, "/")+"/") {
			return l.Label + " · " + T("backup_dir_custom")
		}
	}
	return T("backup_dir_custom")
}

// backupPlace names the backup folder in use briefly: a preset's card
// ("TF2"), a folder on a card ("TF2:/rgdsplus-charge-tune-backups"), or the
// folder.
func (e *Env) backupPlace() string { return e.placeOf(e.BackupDir()) }

// placeOf names a folder (host path) briefly, as backupPlace does.
func (e *Env) placeOf(host string) string {
	dir := e.devicePath(host)
	for _, l := range e.Platform.BackupLocations {
		if l.Dir == dir {
			return l.Label
		}
	}
	for _, l := range e.Platform.BackupLocations {
		card := strings.TrimSuffix(l.Card, "/")
		if strings.HasPrefix(dir, card+"/") {
			return l.Label + ":" + strings.TrimPrefix(dir, card)
		}
	}
	return dir
}

// showAbout shows the version, the disclaimer, this console and the
// license on a scrolling screen; A or B closes it.
func (e *Env) showAbout() {
	var backup string
	e.busy(T("checking_backups"), func() { backup = e.backupOfFirmware() })

	build := fmt.Sprintf("%s/%s, %s", runtime.GOOS, runtime.GOARCH, version.GitCommit)
	if t, err := time.Parse(time.RFC3339, version.BuildDate); err == nil { // "unknown" in dev builds
		build += ", " + t.UTC().Format("2006-01-02 15:04 UTC")
	}
	st := e.state
	boot := "?"
	switch {
	case st.bootErr != nil:
		boot = describe(st.bootErr)
	case st.bootSHA256 != "":
		boot = fmt.Sprintf("%s (sha256 %s)", e.valuesName(st.bootValues), st.bootSHA256[:8])
	}
	orUnknown := func(s string) string {
		if s == "" {
			return "?"
		}
		return s
	}
	opts := gaba.DefaultInfoScreenOptions()
	opts.Sections = []gaba.Section{
		gaba.NewDescriptionSection("", T("about_tagline")+"\n"+strings.TrimPrefix(version.Homepage, "https://")),
		gaba.NewDescriptionSection(T("about_disclaimer_title"), T("disclaimer_full")),
		gaba.NewInfoSection(T("about_device_title"), []gaba.MetadataItem{
			{Label: T("about_label_board"), Value: orUnknown(e.Platform.Board())},
			{Label: T("about_label_firmware"), Value: orUnknown(e.Platform.Firmware())},
			{Label: T("about_label_boot"), Value: boot},
			{Label: T("about_label_backup"), Value: backup},
			{Label: T("backup_folder"), Value: e.devicePath(e.BackupDir())},
			{Label: T("data_folder"), Value: e.DataDir},
			{Label: T("about_label_build"), Value: build},
		}),
		gaba.NewDescriptionSection(T("about_licenses_title"), T("about_licenses")),
	}
	_, _ = gaba.DetailScreen(AppName+" "+version.Version, opts, footer(btn("B", T("back"))))
}

// backupOfFirmware is the verified backup of the firmware in the boot
// partition, or "none yet".
func (e *Env) backupOfFirmware() string {
	if e.state.bootBase != "" {
		for _, b := range e.Engine.ListBackups(e.BackupDir(), e.state.bootBase) {
			if b.Err == nil && b.SameFirmware {
				return e.devicePath(b.Image)
			}
		}
	}
	return T("about_no_backup")
}
