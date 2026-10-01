package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	gaba "github.com/BrandonKowalski/gabagool/v2/pkg/gabagool"

	"rgdsplus-charge-tune/internal/platform"
)

func TestFolderItemsAlwaysDivideActions(t *testing.T) {
	items := folderItems(true, nil)
	last := items[len(items)-1]
	if !last.Separator || last.Text != T("no_subfolders") {
		t.Fatalf("empty folder: last row = %+v, want the no-subfolders divider", last)
	}
	items = folderItems(false, []string{"GBA", "NDS"})
	if len(items) != 5 || !items[2].Separator || items[2].Text != T("folders_header") {
		t.Fatalf("card root rows = %+v", items)
	}
}

func TestFolderItemsActionOrder(t *testing.T) {
	want := []folderRowKind{folderUp, folderNew, folderChoose}
	items := folderItems(true, nil)
	for i, kind := range want {
		if row, _ := items[i].Metadata.(folderRow); row.kind != kind {
			t.Errorf("row %d: kind %d, want %d", i, row.kind, kind)
		}
	}
	root := folderItems(false, nil) // no ".." at the card root
	if row, _ := root[0].Metadata.(folderRow); row.kind != folderNew {
		t.Errorf("card root starts with kind %d, want new folder", row.kind)
	}
}

func TestFolderCursor(t *testing.T) {
	subs := folderItems(true, []string{"GBA", "NDS", "SFC"}) // .., new, choose, divider, GBA=4, NDS=5, SFC=6
	empty := folderItems(true, nil)                          // .., new, choose=2, divider
	cases := []struct {
		name  string
		items []gaba.MenuItem
		saved listPos
		seen  bool
		from  string
		want  listPos
	}{
		{"first visit: first subfolder", subs, listPos{}, false, "", listPos{selected: 4}},
		{"no subfolders: choose this folder", empty, listPos{}, false, "", listPos{selected: 2}},
		{"card root, no subfolders", folderItems(false, nil), listPos{}, false, "", listPos{selected: 1}},
		{"revisit: last position", subs, listPos{selected: 1, visibleStart: 0}, true, "", listPos{selected: 1}},
		{"up: the folder just left", subs, listPos{}, false, "SFC", listPos{selected: 6}},
		{"up: keeps the saved window", subs, listPos{selected: 5, visibleStart: 2}, true, "NDS", listPos{selected: 5, visibleStart: 2}},
		{"up: left folder gone", subs, listPos{}, false, "PSP", listPos{selected: 4}},
		{"stale position", empty, listPos{selected: 9}, true, "", listPos{selected: 2}},
	}
	for _, c := range cases {
		if got := folderCursor(c.items, c.saved, c.seen, c.from); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

// The backup folder screen: every choice is followed by its path on an info
// line, the current choice is marked and selected, and a folder chosen on a
// card before stays in the list.
func TestBackupDirItems(t *testing.T) {
	root := t.TempDir()
	p, err := platform.Load(platform.Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(root, "mnt/sdcard"), 0o755) // TF2 only
	e := &Env{Platform: p}

	items, sel := e.backupDirItems("")
	if !strings.HasPrefix(items[sel].Text, markActive) || items[sel].Metadata.(backupPick).dir != "" {
		t.Fatalf("automatic not selected: %+v", items[sel])
	}
	for i, it := range items {
		if _, ok := it.Metadata.(backupPick); ok && it.Metadata.(backupPick).browse == nil {
			if i+1 >= len(items) || !items[i+1].Info {
				t.Errorf("row %q has no path line after it", it.Text)
			}
		}
	}
	browse := 0
	for _, it := range items {
		if pick, ok := it.Metadata.(backupPick); ok && pick.browse != nil {
			browse++
			if pick.browse.ID != "tf2" {
				t.Errorf("browse offered for %s without its card", pick.browse.ID)
			}
		}
	}
	if browse != 1 {
		t.Fatalf("%d browse rows, want 1 (TF2)", browse)
	}

	custom := "/mnt/sdcard/" + DefaultBackupFolderName
	items, sel = e.backupDirItems(custom)
	if pick := items[sel].Metadata.(backupPick); pick.dir != custom || !items[sel+1].Info || !strings.Contains(items[sel+1].Text, custom) {
		t.Fatalf("custom folder row %+v / %+v", items[sel], items[sel+1])
	}
}
