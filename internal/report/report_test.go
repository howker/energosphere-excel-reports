package report

import (
	"bytes"
	"fmt"
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
		// Fonts and borders survive; wrapping clones are intentionally appended.
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

func TestBatchOptionsAndTrueBlankCells(t *testing.T) {
	p := example()
	options := passport.Options{OmitCompany: true, CompilationDate: "08.10.2026", Commission: []passport.Member{{Role: "Инженер АОСС", Name: "С.Е. Кудряшов"}, {Role: "Начальник", Name: "И.И. Иванов"}}}
	b, e := BuildWithOptions(Template, p, options)
	if e != nil {
		t.Fatal(e)
	}
	w, e := xlsx.FromBytes(b)
	if e != nil {
		t.Fatal(e)
	}
	for _, s := range w.Sheets {
		if s.Name != "Лист1" {
			continue
		}
		for ref, want := range map[string]string{"W14": `ООО "Газпром энерго"`, "Z14": "Клеммная крышка счётчика", "AB14": `ООО "Газпром энерго"`, "Z19": "08.10.2026", "W27": "Инженер АОСС  ________  С.Е. Кудряшов", "W28": "Начальник  ________  И.И. Иванов", "A11": p.Company} {
			got, _ := w.Raw(s.Cells[ref])
			if got != want {
				t.Errorf("%s=%q want %q", ref, got, want)
			}
		}
		for _, c := range s.Cells {
			if c.Type == "inlineStr" && c.Inline.String() == "" {
				t.Errorf("empty inline string blocks overflow: %s", c.Ref)
			}
		}
		if !bytes.Contains(w.Parts[s.Part], []byte(`ref="K1:P1"`)) {
			t.Fatal("heading range missing")
		}
		for _, row := range s.Rows {
			last := 0
			for _, c := range row.Cells {
				col, _ := xlsx.Coordinates(c.Ref)
				if col <= last {
					t.Errorf("unordered cells: row %d ref %s", row.Number, c.Ref)
				}
				last = col
			}
		}
	}
	if FilenameWithOptions(p, options) != "ГПП-1 ЗРУ 6кВ№1 яч.1 РП-17.xlsx" {
		t.Fatal(FilenameWithOptions(p, options))
	}
}
func TestPatchSelfClosingCell(t *testing.T) {
	b := []byte(`<row><c r="A1"/><c r="B1" s="2"><v>42</v></c><c r="C1"><v>7</v></c></row>`)
	b = patchCells(b, map[string]string{"A1": "", "B1": "next", "C1": "001"})
	if !bytes.Contains(b, []byte(`r="B1" s="2"`)) || !bytes.Contains(b, []byte(`>001</t>`)) {
		t.Fatal(string(b))
	}
}

func TestLargeCommissionExtendsForm(t *testing.T) {
	options := passport.Options{}
	for i := 0; i < 25; i++ {
		options.Commission = append(options.Commission, passport.Member{Role: "Инженер", Name: fmt.Sprintf("Участник %d", i+1)})
	}
	b, err := BuildWithOptions(Template, example(), options)
	if err != nil {
		t.Fatal(err)
	}
	w, err := xlsx.FromBytes(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range w.Sheets {
		if s.Name != "Лист1" {
			continue
		}
		for i, member := range options.Commission {
			ref := fmt.Sprintf("W%d", 27+i)
			got, _ := w.Raw(s.Cells[ref])
			if got != member.Signature() {
				t.Errorf("%s=%q", ref, got)
			}
		}
		last := 0
		for _, row := range s.Rows {
			if row.Number <= last {
				t.Fatalf("unordered row %d after %d", row.Number, last)
			}
			last = row.Number
		}
		if !bytes.Contains(w.Parts[s.Part], []byte(`ref="W51:AD51"`)) || !bytes.Contains(w.Parts["xl/workbook.xml"], []byte("$AD$51")) {
			t.Fatal("commission outside print area")
		}
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
