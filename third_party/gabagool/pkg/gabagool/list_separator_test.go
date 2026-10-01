package gabagool

import "testing"

func TestNextSelectableSkipsSeparators(t *testing.T) {
	items := []MenuItem{{Text: "a"}, {Text: "b"}, {Separator: true}, {Text: "c"}}
	cases := []struct{ index, step, want int }{
		{2, 1, 3},  // down onto the divider -> next row
		{2, -1, 1}, // up onto the divider -> previous row
		{0, 1, 0},  // already selectable
	}
	for _, c := range cases {
		if got := nextSelectable(items, c.index, c.step); got != c.want {
			t.Errorf("nextSelectable(%d, %d) = %d, want %d", c.index, c.step, got, c.want)
		}
	}
	wrap := []MenuItem{{Separator: true}, {Text: "x"}}
	if got := nextSelectable(wrap, 0, -1); got != 1 {
		t.Errorf("wrap-around = %d, want 1", got)
	}
}

func TestNextSelectableSkipsInfoLines(t *testing.T) {
	items := []MenuItem{{Info: true, Text: "4.40 V"}, {Info: true, Text: "1500 mA"}, {Separator: true}, {Text: "mode"}, {Text: "settings"}}
	cases := []struct{ index, step, want int }{
		{0, 1, 3},  // initial selection on an info line -> first mode
		{2, -1, 4}, // up from the first mode wraps past the info lines
		{3, 1, 3},  // already selectable
	}
	for _, c := range cases {
		if got := nextSelectable(items, c.index, c.step); got != c.want {
			t.Errorf("nextSelectable(%d, %d) = %d, want %d", c.index, c.step, got, c.want)
		}
	}
	lc := &listController{Options: ListOptions{Items: items}, SelectedItems: map[int]bool{}}
	lc.toggleSelection(0)
	lc.selectAll()
	if items[0].Selected || items[1].Selected || !items[3].Selected {
		t.Fatalf("multi-select touched info lines: %+v", items)
	}
	if got := lc.formatItemText(items[0], true); got != "4.40 V" {
		t.Fatalf("info line in multi-select mode: %q", got)
	}
}

func TestRevealSelectedCentresOffscreenItem(t *testing.T) {
	cases := []struct {
		name              string
		items, sel, start int
		wantStart         int
	}{
		{"already visible", 56, 5, 0, 0},
		{"restored window kept", 56, 34, 30, 30},
		{"below the window", 56, 34, 0, 30},
		{"near the end keeps the window full", 56, 55, 0, 47},
		{"above the window", 56, 2, 20, 0},
		{"short list", 5, 4, 0, 0},
	}
	for _, c := range cases {
		lc := &listController{Options: ListOptions{
			Items:             make([]MenuItem, c.items),
			SelectedIndex:     c.sel,
			VisibleStartIndex: c.start,
			MaxVisibleItems:   9,
		}}
		lc.revealSelected()
		if got := lc.Options.VisibleStartIndex; got != c.wantStart {
			t.Errorf("%s: VisibleStartIndex = %d, want %d", c.name, got, c.wantStart)
		}
	}
}
