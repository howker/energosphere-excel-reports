package xlsx

import "testing"

func TestDatesAndIdentifiers(t *testing.T) {
	w := Workbook{Formats: []string{"", "00000000"}}
	for _, c := range []struct {
		Cell Cell
		Date bool
		Want string
	}{
		{Cell{Value: "1"}, true, "01.01.1900"},
		{Cell{Value: "0"}, true, "0"},
		{Cell{Value: "60"}, true, "29.02.1900"},
		{Cell{Value: "61.958333333"}, true, "01.03.1900"},
		{Cell{Value: "0", Style: 1}, false, "00000000"},
		{Cell{Value: "1111479", Style: 1}, false, "01111479"},
		{Cell{Type: "inlineStr", Inline: RichText{Text: "00123"}}, false, "00123"},
		{Cell{Type: "inlineStr", Inline: RichText{Text: "04.08.2021"}}, true, "04.08.2021"},
	} {
		got, e := w.Text(c.Cell, c.Date)
		if e != nil || got != c.Want {
			t.Errorf("got %q, want %q (%v)", got, c.Want, e)
		}
	}
	w.Date1904 = true
	got, _ := w.Text(Cell{Value: "0"}, true)
	if got != "01.01.1904" {
		t.Fatal(got)
	}
}
