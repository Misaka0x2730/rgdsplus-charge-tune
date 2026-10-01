package internal

import (
	"reflect"
	"strings"
	"testing"
)

func TestWrapPieces(t *testing.T) {
	tests := []struct {
		in   string
		want []string // pieces as sep+text
	}{
		{"Stop  downloading?", []string{"Stop", " downloading?"}},
		{"", nil},
		// every CJK character is a break point, punctuation sticks to its neighbour
		{"停止しますか？", []string{"停", "止", "し", "ま", "す", "か？"}},
		{"サーバー「%s」を削除", []string{"サー", "バー", "「%s」", "を", "削", "除"}},
		{"%s に接続中…", []string{"%s", " に", "接", "続", "中…"}},
		{"(任意)", []string{"(任", "意)"}},
		// punctuation set off by spaces stays with its word
		{"Supprimer « %s » ?", []string{"Supprimer", " « %s » ?"}},
		// a no-break space keeps a number with its unit
		{"use a 5\u00a0V, 3\u00a0A charger", []string{"use", " a", " 5\u00a0V,", " 3\u00a0A", " charger"}},
	}
	for _, tt := range tests {
		var got []string
		for _, p := range WrapPieces(tt.in) {
			got = append(got, p.Sep+p.Text)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("WrapPieces(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if strings.Join(got, "") != strings.Join(strings.FieldsFunc(tt.in, isBreakingSpace), " ") {
			t.Errorf("WrapPieces(%q) does not rebuild the text", tt.in)
		}
	}
}
