package ui

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	gaba "github.com/BrandonKowalski/gabagool/v2/pkg/gabagool"
	"github.com/BrandonKowalski/gabagool/v2/pkg/gabagool/constants"

	"rgdsplus-charge-tune/internal/platform"
)

// DefaultBackupFolderName is offered when a new folder is made for backups.
const DefaultBackupFolderName = "rgdsplus-charge-tune-backups"

// backupPick is what a row of the backup folder screen chooses.
type backupPick struct {
	dir    string                      // device path; "" = automatic
	browse *platform.AvailableLocation // pick a folder on this card instead
}

// chooseBackupDir shows where backups can go: automatic, each card's preset
// folder, the folder chosen before, or one picked on a card. Every choice
// shows its path on a line of its own. It returns the device path ("" for
// automatic).
func (e *Env) chooseBackupDir(current string) (string, bool) {
	for {
		items, sel := e.backupDirItems(current)
		opts := gaba.DefaultListOptions(T("s_backup_dir"), items)
		opts.ItemHeight = 47
		opts.SelectedIndex = sel
		opts.FooterHelpItems = footer(btn("B", T("back")), btn("A", T("select")))
		res, err := gaba.List(opts)
		if err != nil || e.QuitRequested() || len(res.Selected) == 0 {
			return "", false
		}
		pick, ok := items[res.Selected[0]].Metadata.(backupPick)
		if !ok {
			continue
		}
		if pick.browse == nil {
			return pick.dir, true
		}
		loc := *pick.browse
		start := ""
		if host := e.Platform.FSPath(current); current != "" {
			if rel, err := filepath.Rel(loc.Card, host); err == nil && !strings.HasPrefix(rel, "..") {
				start = filepath.ToSlash(rel)
			}
		}
		rel, ok := e.browseFolder(loc, start)
		if !ok {
			continue
		}
		host := filepath.Join(loc.Card, filepath.FromSlash(rel))
		if err := e.Platform.CheckBackupDir(host); err != nil {
			info(errorMessage(T("err_backup_dir"), err))
			continue
		}
		return e.devicePath(host), true
	}
}

// backupDirItems builds the rows and the row to start on (the current
// choice).
func (e *Env) backupDirItems(current string) ([]gaba.MenuItem, int) {
	var items []gaba.MenuItem
	sel := -1
	add := func(label, path string, pick backupPick, chosen bool) {
		mark := markNone
		if chosen {
			mark = markActive
			sel = len(items)
		}
		items = append(items,
			gaba.MenuItem{Text: mark + " " + label, Metadata: pick},
			gaba.MenuItem{Text: "    " + path, Info: true})
	}
	add(T("backup_dir_auto_row"), T("backup_dir_now", e.devicePath(e.Platform.DefaultBackupDir())), backupPick{}, current == "")
	preset := false
	locs := e.Platform.Locations()
	for _, l := range locs {
		dev := e.devicePath(l.Dir)
		label := l.Label
		if !l.Present {
			label += " (" + T("no_card") + ")"
		}
		add(label, dev, backupPick{dir: dev}, current == dev)
		preset = preset || current == dev
	}
	if current != "" && !preset {
		add(T("backup_dir_custom"), current, backupPick{dir: current}, true)
	}
	items = append(items, gaba.MenuItem{Separator: true})
	for i := range locs {
		if locs[i].Present {
			items = append(items, gaba.MenuItem{Text: iconFolder + "  " + T("backup_dir_browse", locs[i].Label),
				Metadata: backupPick{browse: &locs[i]}})
		}
	}
	if sel < 0 {
		sel = 0
	}
	return items, sel
}

type folderRowKind int

const (
	folderChoose folderRowKind = iota
	folderUp
	folderNew
	folderSub
)

type folderRow struct {
	kind folderRowKind
	name string
}

type listPos struct{ selected, visibleStart int }

// browseFolder lets the user pick a folder on a card by walking the tree
// ("..", "new folder", "choose this folder", subfolders), as in DSFetch.
// rel is the start folder relative to the card root; if it does not exist
// the nearest existing parent is shown. START picks the folder shown, B
// cancels.
func (e *Env) browseFolder(card platform.AvailableLocation, rel string) (string, bool) {
	cur := cleanRel(rel)
	for cur != "" && !dirExists(filepath.Join(card.Card, filepath.FromSlash(cur))) {
		cur = parentRel(cur)
	}
	visited := map[string]listPos{}
	from := "" // the subfolder just left with ".."

	for {
		items := folderItems(cur != "", subfolders(filepath.Join(card.Card, filepath.FromSlash(cur))))
		saved, seen := visited[cur]
		pos := folderCursor(items, saved, seen, from)
		from = ""

		opts := gaba.DefaultListOptions(card.Label+":/"+cur, items)
		opts.UseSmallTitle = true
		opts.SelectedIndex, opts.VisibleStartIndex = pos.selected, pos.visibleStart
		opts.ActionButton = constants.VirtualButtonStart
		opts.MultiSelectConfirmButton = constants.VirtualButtonUnassigned
		opts.FooterHelpItems = footer(btn("B", T("cancel")), btn("A", T("open")), startBtn(T("select")))
		res, err := gaba.List(opts)
		if err != nil || e.QuitRequested() {
			return "", false
		}
		if res.Action == gaba.ListActionTriggered { // START
			return cur, true
		}
		if len(res.Selected) == 0 {
			return "", false
		}
		idx := res.Selected[0]
		visited[cur] = listPos{selected: idx, visibleStart: max(0, idx-res.VisiblePosition)}
		row, ok := items[idx].Metadata.(folderRow)
		if !ok {
			continue // a divider
		}
		switch row.kind {
		case folderChoose:
			return cur, true
		case folderUp:
			from = path.Base(cur)
			cur = parentRel(cur)
		case folderSub:
			cur = path.Join(cur, row.name)
		case folderNew:
			name, ok := e.askFolderName()
			if !ok {
				continue
			}
			sub := path.Join(cur, name)
			if err := os.MkdirAll(filepath.Join(card.Card, filepath.FromSlash(sub)), 0o755); err != nil {
				info(errorMessage(T("err_mkdir", name), err))
				continue
			}
			cur = sub
		}
	}
}

// folderItems builds the browser rows: the actions ("..", "new folder",
// "choose this folder" next to the subfolders), a divider that always closes
// them (it says so when there are no subfolders), then subfolders.
func folderItems(hasParent bool, subs []string) []gaba.MenuItem {
	var items []gaba.MenuItem
	if hasParent {
		items = append(items, gaba.MenuItem{Text: iconUp + "  ..", Metadata: folderRow{kind: folderUp}})
	}
	items = append(items,
		gaba.MenuItem{Text: "+  " + T("new_folder"), Metadata: folderRow{kind: folderNew}},
		gaba.MenuItem{Text: "✓  " + T("choose_this_folder"), Metadata: folderRow{kind: folderChoose}},
	)
	divider := T("folders_header")
	if len(subs) == 0 {
		divider = T("no_subfolders")
	}
	items = append(items, gaba.MenuItem{Text: divider, Separator: true})
	for _, name := range subs {
		items = append(items, gaba.MenuItem{Text: iconFolder + "  " + name, Metadata: folderRow{kind: folderSub, name: name}})
	}
	return items
}

// folderCursor picks the starting row: the subfolder just left with ".."
// (from), else the row of the last visit, else the first subfolder, else
// "choose this folder".
func folderCursor(items []gaba.MenuItem, saved listPos, seen bool, from string) listPos {
	first, choose := -1, 0
	for i, it := range items {
		row, ok := it.Metadata.(folderRow)
		if ok && row.kind == folderChoose {
			choose = i
		}
		if !ok || row.kind != folderSub {
			continue
		}
		if first < 0 {
			first = i
		}
		if from != "" && row.name == from {
			if seen && saved.selected == i {
				return saved
			}
			return listPos{selected: i} // the list scrolls it into view
		}
	}
	if seen && saved.selected < len(items) {
		return saved
	}
	if first >= 0 {
		return listPos{selected: first}
	}
	return listPos{selected: choose}
}

// askFolderName reads a new folder name with the on-screen keyboard,
// offering DefaultBackupFolderName.
func (e *Env) askFolderName() (string, bool) {
	res, err := gaba.Keyboard(DefaultBackupFolderName, T("help_exit"))
	if err != nil || e.QuitRequested() {
		return "", false
	}
	name := platform.SafeFileName(strings.TrimSpace(res.Text))
	if name == "_" || name == "" {
		return "", false
	}
	return name, true
}

func subfolders(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() && !strings.HasPrefix(n, ".") && n != "System Volume Information" {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func cleanRel(rel string) string {
	rel = strings.ReplaceAll(rel, "\\", "/")
	return strings.Trim(path.Clean("/"+rel), "/")
}

func parentRel(rel string) string {
	p := path.Dir(rel)
	if p == "." || p == "/" {
		return ""
	}
	return p
}
