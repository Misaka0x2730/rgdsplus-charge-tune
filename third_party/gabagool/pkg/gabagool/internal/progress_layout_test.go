package internal

import "testing"

// The message block and the bar never overlap, whatever the line count, and
// the whole group stays centred.
func TestProgressLayout(t *testing.T) {
	const winH, lineH, gap, barH = int32(768), int32(44), int32(12), int32(40)
	for lines := 0; lines <= 4; lines++ {
		textY, barY := ProgressLayout(winH, lineH, lines, gap, barH)
		textTop := textY - int32(lines)*lineH/2 // as RenderMultilineText starts
		textBottom := textTop
		if lines > 0 {
			textBottom = textTop + int32(lines)*lineH + int32(lines-1)*5
		}
		if barY < textBottom+gap {
			t.Errorf("%d lines: bar at %d overlaps text ending at %d", lines, barY, textBottom)
		}
		if above, below := textTop, winH-(barY+barH); above-below > 1 || below-above > 1 {
			t.Errorf("%d lines: not centred (%d above, %d below)", lines, above, below)
		}
	}
}
