package report

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/howker/energosphere-excel-reports/internal/passport"
	"github.com/howker/energosphere-excel-reports/internal/xlsx"
)

var cellRE = regexp.MustCompile(`(?s)<c\b[^>]*?(?:/>|>.*?</c>)`)
var refRE = regexp.MustCompile(`\br="([^"]+)"`)
var styleRE = regexp.MustCompile(`\bs="([^"]+)"`)
var dataRE = regexp.MustCompile(`(?s)<sheetData\b[^>]*>.*?</sheetData>`)
var pictureRE = regexp.MustCompile(`<xdr:cNvPr\b[^>]*name="Рисунок[123]"[^>]*/>`)
var hiddenRE = regexp.MustCompile(` hidden="[^"]*"`)
var relationRE = regexp.MustCompile(`<Relationship\b[^>]*/>`)
var overrideRE = regexp.MustCompile(`<Override\b[^>]*/>`)

func escape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
func textCell(ref, style, value string) string {
	if style != "" {
		style = ` s="` + style + `"`
	}
	if value == "" {
		return `<c r="` + ref + `"` + style + `/>`
	}
	return `<c r="` + ref + `"` + style + ` t="inlineStr"><is><t xml:space="preserve">` + escape(value) + `</t></is></c>`
}
func patchCells(data []byte, values map[string]string) []byte {
	return cellRE.ReplaceAllFunc(data, func(b []byte) []byte {
		ref := refRE.FindSubmatch(b)
		if len(ref) < 2 {
			return b
		}
		value, ok := values[string(ref[1])]
		if !ok {
			return b
		}
		style := ""
		if s := styleRE.FindSubmatch(b); len(s) > 1 {
			style = string(s[1])
		}
		return []byte(textCell(string(ref[1]), style, value))
	})
}
func present(e passport.Equipment) bool { return e.Type != "" && e.Type != "-" }
func Scheme(p passport.Passport) string {
	if present(p.CT[0]) && present(p.CT[2]) && present(p.VT[0]) && present(p.VT[2]) {
		if !present(p.CT[1]) {
			if !present(p.VT[1]) {
				return "Рисунок3"
			}
			return "Рисунок2"
		}
		if present(p.VT[1]) {
			return "Рисунок1"
		}
	}
	return ""
}
func printValues(p passport.Passport, options passport.Options) map[string]string {
	v := map[string]string{
		"Q1": "Трансформаторы напряжения:", "A11": p.Company, "A16": p.Object, "A18": p.Connection,
		"M3": p.Connection, "M4": p.Accounting, "M5": p.Meter.Type, "P5": p.Meter.Serial,
		"M7": p.Meter.Accuracy, "P12": p.Meter.Year, "M12": p.Meter.Verification,
		"E10": strings.TrimSpace(strings.TrimSuffix(p.Voltage, "кВ")), "K26": p.Ownership,
		"K33": "Фаза B", "M13": "",
	}
	// Explicitly clear constants and example observations, including conclusions.
	for _, a := range strings.Fields("P4 M6 P6 P7 M9 P9 M10 P10 P13 M15 P15 M20 W5 Y5 AA5 W14 Z14 AB14 W15 Z15 AB15 W21 B44 Q41 Q43 C30 E31") {
		v[a] = ""
	}
	for i := 0; i < 3; i++ {
		t := p.CT[i]
		n := 28 + 6*i
		v[fmt.Sprintf("L%d", n)] = t.Type
		v[fmt.Sprintf("N%d", n)] = t.Serial
		v[fmt.Sprintf("P%d", n)] = t.Accuracy
		v[fmt.Sprintf("L%d", n+1)] = t.Ratio
		v[fmt.Sprintf("N%d", n+1)] = ""
		v[fmt.Sprintf("P%d", n+1)] = ""
		v[fmt.Sprintf("L%d", n+3)] = ""
		v[fmt.Sprintf("N%d", n+3)] = t.Verification
		v[fmt.Sprintf("P%d", n+3)] = ""
		u := p.VT[i]
		n = 4 + 6*i
		v[fmt.Sprintf("R%d", n)] = u.Type
		v[fmt.Sprintf("T%d", n)] = u.Serial
		v[fmt.Sprintf("V%d", n)] = u.Accuracy
		v[fmt.Sprintf("R%d", n+1)] = u.Ratio
		v[fmt.Sprintf("T%d", n+1)] = ""
		v[fmt.Sprintf("V%d", n+1)] = ""
		v[fmt.Sprintf("R%d", n+3)] = ""
		v[fmt.Sprintf("T%d", n+3)] = u.Verification
		v[fmt.Sprintf("V%d", n+3)] = ""
	}
	phases := []string{}
	for i, t := range p.CT {
		if present(t) {
			phases = append(phases, []string{"A", "B", "C"}[i])
		}
	}
	v["M13"] = strings.Join(phases, ", ")
	v["W14"] = `ООО "Газпром энерго"`
	v["Z14"] = "Клеммная крышка счётчика"
	v["AB14"] = `ООО "Газпром энерго"`
	v["Z19"] = options.CompilationDate
	for i, m := range options.Commission {
		v[fmt.Sprintf("W%d", 27+i)] = m.Signature()
	}
	return v
}

var sourceHeaders = []string{"№", "Общество", "Объект", "Инвентарный №", "Присоединение", "Напряжение", "Тип учёта", "Тип счётчика", "№ счётчика", "Класс счётчика", "Год счётчика", "Поверка счётчика", "Следующая поверка счётчика", "Свидетельство счётчика", "Тип ТТ", "Отношение ТТ", "№ ТТ", "Класс ТТ", "Год ТТ", "Поверка ТТ", "Следующая поверка ТТ", "Свидетельство ТТ", "Тип ТН", "Отношение ТН", "№ ТН", "Класс ТН", "Год ТН", "Поверка ТН", "Следующая поверка ТН", "Свидетельство ТН", "Информационный обмен", "Принадлежность"}

func sourceData(p passport.Passport) []byte {
	var b strings.Builder
	b.WriteString(`<sheetData><row r="1">`)
	for i, h := range sourceHeaders {
		b.WriteString(textCell(xlsx.Column(i+1)+"1", "", h))
	}
	b.WriteString(`</row>`)
	for r := 0; r < 3; r++ {
		b.WriteString(`<row r="` + strconv.Itoa(r+3) + `">`)
		for c, value := range p.SourceCells[r] {
			b.WriteString(textCell(xlsx.Column(c+1)+strconv.Itoa(r+3), "", value))
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData>`)
	return []byte(b.String())
}

// Build fills the form and adjusts text wrapping and drawing visibility.
// Native pictures, fonts, borders and paper settings remain in the ZIP.
func Build(template []byte, p passport.Passport) ([]byte, error) {
	return BuildWithOptions(template, p, passport.Options{})
}
func BuildWithOptions(template []byte, p passport.Passport, options passport.Options) ([]byte, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	w, err := xlsx.FromBytes(template)
	if err != nil {
		return nil, err
	}
	found := false
	for _, s := range w.Sheets {
		values := make(map[string]string)
		for ref, c := range s.Cells {
			value, e := w.Raw(c)
			if e != nil {
				return nil, e
			}
			values[ref] = value
			if c.Formula != "" {
				values[ref] = ""
			}
		}
		switch s.Name {
		case "Лист1":
			found = true
			for ref, value := range printValues(p, options) {
				values[ref] = value
			}
		case "исх.данные":
			w.Parts[s.Part] = dataRE.ReplaceAllLiteral(w.Parts[s.Part], sourceData(p))
			continue
		default: // Old database metadata and lookup example data are not sources.
			for ref := range values {
				values[ref] = ""
			}
		}
		w.Parts[s.Part] = patchCells(w.Parts[s.Part], values)
		if s.Name == "Лист1" {
			if err := layout(w, s, values, options); err != nil {
				return nil, err
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("шаблон не содержит Лист1")
	}
	for name, data := range w.Parts {
		if strings.HasPrefix(name, "xl/drawings/drawing") && strings.HasSuffix(name, ".xml") {
			selected := Scheme(p)
			data = pictureRE.ReplaceAllFunc(data, func(b []byte) []byte {
				b = hiddenRE.ReplaceAll(b, nil)
				if !bytes.Contains(b, []byte(`name="`+selected+`"`)) || selected == "" {
					b = bytes.Replace(b, []byte("/>"), []byte(` hidden="1"/>`), 1)
				}
				return b
			})
		}
		if strings.HasSuffix(name, ".rels") {
			data = relationRE.ReplaceAllFunc(data, func(b []byte) []byte {
				if bytes.Contains(b, []byte("vbaProject")) || bytes.Contains(b, []byte("calcChain")) || bytes.Contains(b, []byte("sharedStrings")) {
					return nil
				}
				return b
			})
		}
		if name == "[Content_Types].xml" {
			data = bytes.ReplaceAll(data, []byte("application/vnd.ms-excel.sheet.macroEnabled.main+xml"), []byte("application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"))
			data = overrideRE.ReplaceAllFunc(data, func(b []byte) []byte {
				if bytes.Contains(b, []byte("vbaProject")) || bytes.Contains(b, []byte("calcChain")) || bytes.Contains(b, []byte("sharedStrings")) {
					return nil
				}
				return b
			})
		}
		if name == "xl/workbook.xml" {
			data = bytes.ReplaceAll(data, []byte(`name="Print_Area"`), []byte(`name="_xlnm.Print_Area"`))
		}
		w.Parts[name] = data
	}
	delete(w.Parts, "xl/vbaProject.bin")
	delete(w.Parts, "xl/calcChain.xml")
	delete(w.Parts, "xl/sharedStrings.xml")
	// Example provenance is not provenance of newly generated passports.
	delete(w.Parts, "docProps/custom.xml")
	for name, data := range w.Parts {
		if strings.HasSuffix(name, ".rels") {
			w.Parts[name] = relationRE.ReplaceAllFunc(data, func(b []byte) []byte {
				if bytes.Contains(b, []byte("custom-properties")) {
					return nil
				}
				return b
			})
		}
	}
	w.Parts["[Content_Types].xml"] = overrideRE.ReplaceAllFunc(w.Parts["[Content_Types].xml"], func(b []byte) []byte {
		if bytes.Contains(b, []byte("/docProps/custom.xml")) {
			return nil
		}
		return b
	})
	w.Parts["docProps/core.xml"] = []byte(`<?xml version="1.0" encoding="UTF-8"?><cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:creator>EnergySphere Excel Reports</dc:creator></cp:coreProperties>`)
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	names := []string{}
	for n := range w.Parts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		f, e := z.Create(n)
		if e != nil {
			return nil, e
		}
		if _, e = f.Write(w.Parts[n]); e != nil {
			return nil, e
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func Filename(p passport.Passport, prefix string) string {
	return FilenameWithOptions(p, passport.Options{Prefix: prefix})
}
func FilenameWithOptions(p passport.Passport, options passport.Options) string {
	if options.OmitCompany {
		p.Company = ""
	}
	s := p.Chain(options.Prefix)
	s = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return ' '
		}
		return r
	}, s)
	s = strings.TrimRight(strings.Join(strings.Fields(s), " "), ". ")
	if s == "" {
		s = fmt.Sprintf("Присоединение строка %d", p.Row)
	}
	base := strings.ToUpper(strings.Split(s, ".")[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || regexp.MustCompile(`^(COM|LPT)[1-9]$`).MatchString(base) {
		s = "_" + s
	}
	return s + ".xlsx"
}
func Save(template []byte, p passport.Passport, dir, prefix string) (string, error) {
	return SaveWithOptions(template, p, dir, passport.Options{Prefix: prefix})
}
func SaveWithOptions(template []byte, p passport.Passport, dir string, options passport.Options) (string, error) {
	data, err := BuildWithOptions(template, p, options)
	if err != nil {
		return "", err
	}
	name := FilenameWithOptions(p, options)
	stem := strings.TrimSuffix(name, ".xlsx")
	for i := 0; i < 10000; i++ {
		n := name
		if i > 0 {
			n = fmt.Sprintf("%s (стр.%d-%d).xlsx", stem, p.Row, i)
		}
		full := filepath.Join(dir, n)
		absolute, e := filepath.Abs(full)
		if e != nil {
			return "", e
		}
		if len(utf16.Encode([]rune(absolute))) >= 260 || len(utf16.Encode([]rune(n))) > 255 {
			return "", fmt.Errorf("полное имя слишком длинное для Windows 7; выберите короткую папку или уменьшите общий префикс")
		}
		f, e := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0666)
		if os.IsExist(e) {
			continue
		}
		if e != nil {
			return "", e
		}
		_, e = io.Copy(f, bytes.NewReader(data))
		closeErr := f.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil {
			os.Remove(full)
			return "", e
		}
		return full, nil
	}
	return "", fmt.Errorf("слишком много совпадений имени")
}
