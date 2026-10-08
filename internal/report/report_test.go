package report

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/howker/energosphere-excel-reports/internal/passport"
	"github.com/howker/energosphere-excel-reports/internal/xlsx"
)

func example() passport.Passport {
	p := passport.Passport{Row: 10, Company: "Астраханский ГПЗ", Object: "ГПП-1", Connection: "ЗРУ 6кВ№1 яч.1 РП-17", Voltage: "6 кВ", Meter: passport.Equipment{Type: "Счётчик", Serial: "001234", Verification: "04.08.2021", Accuracy: "0,2S"}}
	p.CT[0] = passport.Equipment{Type: "ТТ", Ratio: "50/1", Serial: "00007", Verification: "01.01.2026"}
	p.CT[2] = p.CT[0]
	for i := range p.VT {
		p.VT[i] = passport.Equipment{Type: "ТН", Ratio: "6000/100", Serial: "100"}
	}
	p.SourceCells[0][8] = p.Meter.Serial
	p.SourceCells[0][12] = "12.09.2075"
	return p
}
func TestTemplateAndRender(t *testing.T) {
	p := example()
	b, e := Build(Template, p)
	if e != nil {
		t.Fatal(e)
	}
	w, e := xlsx.FromBytes(b)
	if e != nil {
		t.Fatal(e)
	}
	var s *xlsx.Sheet
	for _, sh := range w.Sheets {
		if sh.Name == "Лист1" {
			s = sh
		}
	}
	if s == nil {
		t.Fatal("missing print sheet")
	}
	expected := map[string]string{"Q1": "Трансформаторы напряжения:", "M5": "Счётчик", "P5": "001234", "M7": "0,2S", "P7": "", "M12": "04.08.2021", "L29": "50/1", "L34": "", "N28": "00007", "N31": "01.01.2026", "T5": "", "W21": "", "B44": "", "P10": "", "A18": p.Connection}
	for ref, value := range expected {
		got, e := w.Raw(s.Cells[ref])
		if e != nil || got != value {
			t.Errorf("%s=%q, want %q; %v", ref, got, value, e)
		}
	}
	for _, sh := range w.Sheets {
		for _, c := range sh.Cells {
			if c.Formula != "" || c.Type == "s" {
				t.Errorf("stale formula/shared string %s %s", sh.Name, c.Ref)
			}
		}
	}
	if _, ok := w.Parts["xl/vbaProject.bin"]; ok {
		t.Fatal("macro retained")
	}
	orig, _ := xlsx.FromBytes(Template)
	for n, d := range orig.Parts {
		if len(n) > 9 && n[:9] == "xl/media/" && !bytes.Equal(d, w.Parts[n]) {
			t.Errorf("changed image %s", n)
		}
		if n == "xl/styles.xml" && !bytes.Equal(d, w.Parts[n]) {
			t.Fatal("changed styles")
		}
	}
	if Scheme(p) != "Рисунок2" {
		t.Fatal("wrong scheme")
	}
	p.VT[1] = passport.Equipment{}
	if Scheme(p) != "Рисунок3" {
		t.Fatal("two VT scheme")
	}
	p.CT[0] = passport.Equipment{}
	if Scheme(p) != "" {
		t.Fatal("unsupported scheme must stay hidden")
	}
}
func TestPatchSelfClosingCell(t *testing.T) {
	b := []byte(`<row><c r="A1"/><c r="B1" s="2"><v>42</v></c><c r="C1"><v>7</v></c></row>`)
	b = patchCells(b, map[string]string{"A1": "", "B1": "next", "C1": "001"})
	if !bytes.Contains(b, []byte(`r="B1" s="2"`)) || !bytes.Contains(b, []byte(`>001</t>`)) {
		t.Fatal(string(b))
	}
}
func TestNoOverwriteAndNames(t *testing.T) {
	p := example()
	dir := t.TempDir()
	a, e := Save(Template, p, dir, "")
	if e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(a)
	b, e := Save(Template, p, dir, "")
	if e != nil {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(a)
	if a == b || !bytes.Equal(before, after) {
		t.Fatal("existing report overwritten")
	}
	if filepath.Base(a) != "Астраханский ГПЗ ГПП-1 ЗРУ 6кВ№1 яч.1 РП-17.xlsx" {
		t.Fatal(a)
	}
	p = passport.Passport{Connection: "CON"}
	if Filename(p, "") != "_CON.xlsx" {
		t.Fatal("reserved name")
	}
	p.Connection = "test:/\\?*"
	if Filename(p, "") != "test.xlsx" {
		t.Fatal(Filename(p, ""))
	}
}
