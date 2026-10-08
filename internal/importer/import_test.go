package importer

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, invalid bool) string {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	parts := map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Переименованный лист" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
	}
	cell := func(ref, text string) string {
		return `<c r="` + ref + `" t="inlineStr"><is><t>` + text + `</t></is></c>`
	}
	x := `<worksheet><sheetData><row r="6">` + cell("B6", "Дочернее общество") + cell("E6", "Наименование точки присоединения") + cell("H6", "Счетчик электрической энергии") + cell("O6", "Трансформатор тока") + cell("W6", "Трансформатор напряжения") + `</row>`
	x += `<row r="10"><c r="A10"><v>1</v></c>` + cell("B10", "Общество") + cell("C10", "Объект") + cell("E10", "Присоединение") + cell("I10", "001234") + `<c r="L10"><v>61.958333</v></c>` + cell("O10", "ТТ") + cell("Q10", "A") + cell("W10", "ТН") + cell("Y10", "общий ТН") + `</row>`
	x += `<row r="11">` + cell("W11", "ТН") + cell("Y11", "общий ТН") + `</row><row r="12">` + cell("O12", "ТТ") + cell("Q12", "C") + cell("W12", "ТН") + cell("Y12", "общий ТН") + `</row>`
	x += `<row r="15">` + cell("E15", "Пояснения к таблице") + `</row></sheetData><mergeCells>`
	last := 12
	if invalid {
		last = 13
	}
	for _, c := range []string{"A", "B", "C", "E", "I", "L"} {
		x += fmt.Sprintf(`<mergeCell ref="%s10:%s%d"/>`, c, c, last)
	}
	x += `</mergeCells></worksheet>`
	parts["xl/worksheets/sheet1.xml"] = x
	for n, data := range parts {
		f, e := z.Create(n)
		if e != nil {
			t.Fatal(e)
		}
		f.Write([]byte(data))
	}
	z.Close()
	p := filepath.Join(t.TempDir(), "source.xlsx")
	if e := os.WriteFile(p, b.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	return p
}
func TestMergedSourceAndPhaseGap(t *testing.T) {
	items, e := Load(fixture(t, false))
	if e != nil {
		t.Fatal(e)
	}
	if len(items) != 1 {
		t.Fatal(len(items))
	}
	p := items[0]
	if p.Meter.Serial != "001234" || p.Meter.Verification != "01.03.1900" || p.Company != "Общество" {
		t.Fatalf("%+v", p)
	}
	if p.CT[1].Type != "" || p.CT[2].Serial != "C" || p.VT[1].Serial != "общий ТН" {
		t.Fatal("positions changed")
	}
}
func TestUnexpectedBlockFails(t *testing.T) {
	if _, e := Load(fixture(t, true)); e == nil {
		t.Fatal("bad block accepted")
	}
}
