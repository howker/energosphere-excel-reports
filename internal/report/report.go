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
var busSectionCutRegex = regexp.MustCompile(`(?i)\s+(?:Ввод|яч\.?|ячейка|Линия|ВЛ|КЛ|Т-\d+|ТДН|ТМН|ТТР|ТСН|Ф-\d+|СВ|ШСВ|ТН|АВР)\b.*`) // Справочник номеров в Госреестре СИ
var gosReestrDict = map[string]string{
	"меркурий 234": "55276-13",
	"меркурий 230": "23345-07",
	"сэт-4тм.03":   "20175-01",
	"сэт-4тм.02":   "20175-01",
	"псч-4тм":      "20175-01",
	"цэ6850":       "16864-08",
	"тлш-10":       "1832-64",
	"тпл-10":       "1254-58",
	"тол-10":       "14652-95",
	"тпол-10":      "1254-58",
	"тлм-10":       "1831-64",
	"твлм-10":      "1831-64",
	"знол-0.6-10":  "3344-72",
	"знол-0,6-10":  "3344-72",
	"знол.06-10":   "3344-72",
	"знол-10":      "3344-72",
	"нами-10":      "3297-72",
	"нтми-10":      "2150-66",
	"намит-10":     "19794-00",
	"нол-10":       "3492-72",
	"нол.06-10":    "3492-72",
	"тфзм":         "1544-61",
}

// Справочник номинальных вторичных нагрузок ТТ (ВА)
var ctNominalBurdenDict = map[string]string{
	"тлш-10":   "20",
	"тпл-10":   "10",
	"тол-10":   "15",
	"тпол-10":  "15",
	"тлм-10":   "15",
	"твлм-10":  "15",
	"тфзм":     "30",
	"тшл-0.66": "5",
	"топ-0.66": "5",
	"тшп-0.66": "5",
}

// Справочник номинальных мощностей ТН (ВА) для класса 0.5
var vtNominalBurdenDict = map[string]string{
	"знол-0.6-10": "75",
	"знол-0,6-10": "75",
	"знол.06-10":  "75",
	"знол-10":     "75",
	"нами-10":     "75",
	"нтми-10":     "120",
	"намит-10":    "150",
	"нол-10":      "75",
	"нол.06-10":   "75",
}

func lookupGosReestr(eqType string) string {
	n := strings.ToLower(strings.TrimSpace(eqType))
	for k, v := range gosReestrDict {
		if strings.Contains(n, k) {
			return v
		}
	}
	return ""
}

func lookupCTNominalBurden(eqType string) string {
	n := strings.ToLower(strings.TrimSpace(eqType))
	for k, v := range ctNominalBurdenDict {
		if strings.Contains(n, k) {
			return v
		}
	}
	return ""
}

func lookupVTNominalBurden(eqType string) string {
	n := strings.ToLower(strings.TrimSpace(eqType))
	for k, v := range vtNominalBurdenDict {
		if strings.Contains(n, k) {
			return v
		}
	}
	return ""
}

func lookupMeterNominals(meterType, accuracy string) (voltage, current, reactiveAccuracy, tariffs string) {
	n := strings.ToLower(strings.TrimSpace(meterType))
	if n == "" || n == "-" || n == "счётчик" {
		return "", "", "", ""
	}
	voltage = "3×57,7/100 В"
	current = "5 (10) А"
	tariffs = "4"

	if strings.Contains(n, "сэт-4тм") || strings.Contains(n, "псч-4тм") {
		current = "5 (7,5) А"
	}

	acc := strings.ToLower(strings.TrimSpace(accuracy))
	if strings.Contains(acc, "0,2") || strings.Contains(acc, "0.2") {
		reactiveAccuracy = "0,5"
	} else if strings.Contains(acc, "0,5") || strings.Contains(acc, "0.5") {
		reactiveAccuracy = "1,0"
	} else {
		reactiveAccuracy = "1,0"
	}
	return
}

func parseYear(s string) int {
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		return r < '0' || r > '9'
	}) {
		if len(part) == 4 && (strings.HasPrefix(part, "19") || strings.HasPrefix(part, "20")) {
			if y, err := strconv.Atoi(part); err == nil {
				return y
			}
		}
	}
	return 0
}

func calcInterval(start, end string) string {
	ys := parseYear(start)
	ye := parseYear(end)
	if ys > 0 && ye > ys {
		diff := ye - ys
		if diff > 30 {
			return "16"
		}
		return strconv.Itoa(diff)
	}
	return ""
}

func ExtractBusSection(conn string) string {
	conn = strings.TrimSpace(conn)
	loc := busSectionCutRegex.FindStringIndex(conn)
	if loc != nil && loc[0] > 0 {
		return strings.TrimSpace(conn[:loc[0]])
	}
	return conn
}

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
	ttPlace := "Трансформаторы тока:"
	meterEnd := ""
	if len(p.SourceCells) > 0 && len(p.SourceCells[0]) > 12 {
		meterEnd = p.SourceCells[0][12]
	}
	meterInterval := calcInterval(p.Meter.Verification, meterEnd)
	if meterInterval == "" && p.Meter.Type != "" {
		meterInterval = "16"
	}

	mVolt, mCurr, mReactAcc, mTariffs := lookupMeterNominals(p.Meter.Type, p.Meter.Accuracy)
	energyType := ""
	syncClocks := ""
	tempRegime := ""
	if p.Meter.Type != "" && p.Meter.Type != "-" && p.Meter.Type != "Счётчик" {
		energyType = "А, R"
		syncClocks = "да"
		tempRegime = "соответствует"
	}
	v := map[string]string{
		"Q1": "Трансформаторы напряжения:", "Q2": "Место установки:", "R2": ExtractBusSection(p.Connection),
		"A11": `Инженерно-технического центра ООО «Газпром энерго»`, "A16": p.Object, "A18": p.Connection,
		"M3": p.Connection, "M4": p.Accounting, "P4": energyType,
		"M5": p.Meter.Type, "P5": p.Meter.Serial,
		"M6": mVolt, "P6": mCurr,
		"M7": p.Meter.Accuracy, "P7": mReactAcc,
		"M9": mTariffs, "P9": lookupGosReestr(p.Meter.Type),
		"M10": syncClocks, "P10": meterInterval,
		"M12": p.Meter.Verification, "P12": p.Meter.Year,
		"P13": tempRegime,
		"E10": strings.TrimSpace(strings.TrimSuffix(p.Voltage, "кВ")),
		"K25": ttPlace, "O25": "", "K26": "Место установки: " + p.Connection,
		"K33": "Фаза B", "M13": "",
	}
	// Explicitly clear constants and example observations
	for _, a := range strings.Fields("B44 Q41 Q43 C30 E31") {
		v[a] = ""
	}
	v["M15"] = "не проводилось"
	v["P15"] = "не проводилось"
	// Перечень средств измерений (Парма ВАФ-А)
	v["W5"] = "Парма ВАФ-А"
	v["Y5"] = "3387"
	v["AA5"] = "1"
	v["AC5"] = options.VafVerification
	for _, a := range strings.Fields("W6 Y6 AA6 AC6") {
		v[a] = ""
	}
	for i := 0; i < 3; i++ {
		t := p.CT[i]
		n := 28 + 6*i
		v[fmt.Sprintf("L%d", n)] = t.Type
		v[fmt.Sprintf("N%d", n)] = t.Serial
		v[fmt.Sprintf("P%d", n)] = t.Accuracy
		v[fmt.Sprintf("L%d", n+1)] = t.Ratio
		v[fmt.Sprintf("N%d", n+1)] = lookupCTNominalBurden(t.Type)
		v[fmt.Sprintf("P%d", n+1)] = ""
		v[fmt.Sprintf("L%d", n+3)] = lookupGosReestr(t.Type)
		v[fmt.Sprintf("N%d", n+3)] = t.Verification
		ctEnd := ""
		if len(p.SourceCells) > i && len(p.SourceCells[i]) > 20 {
			ctEnd = p.SourceCells[i][20]
		}
		if ctEnd == "" && len(p.SourceCells) > 0 && len(p.SourceCells[0]) > 20 {
			ctEnd = p.SourceCells[0][20]
		}
		ctInterval := calcInterval(t.Verification, ctEnd)
		if ctInterval == "" && t.Type != "" && t.Type != "-" {
			ctInterval = "8"
		}
		v[fmt.Sprintf("P%d", n+3)] = ctInterval

		u := p.VT[i]
		n = 4 + 6*i
		v[fmt.Sprintf("R%d", n)] = u.Type
		v[fmt.Sprintf("T%d", n)] = u.Serial
		v[fmt.Sprintf("V%d", n)] = u.Accuracy
		v[fmt.Sprintf("R%d", n+1)] = u.Ratio
		v[fmt.Sprintf("T%d", n+1)] = lookupVTNominalBurden(u.Type)
		v[fmt.Sprintf("V%d", n+1)] = ""
		v[fmt.Sprintf("R%d", n+3)] = lookupGosReestr(u.Type)
		v[fmt.Sprintf("T%d", n+3)] = u.Verification
		vtEnd := ""
		if len(p.SourceCells) > i && len(p.SourceCells[i]) > 28 {
			vtEnd = p.SourceCells[i][28]
		}
		if vtEnd == "" && len(p.SourceCells) > 0 && len(p.SourceCells[0]) > 28 {
			vtEnd = p.SourceCells[0][28]
		}
		vtInterval := calcInterval(u.Verification, vtEnd)
		if vtInterval == "" && u.Type != "" && u.Type != "-" {
			vtInterval = "8"
		}
		v[fmt.Sprintf("V%d", n+3)] = vtInterval
	}
	phases := []string{}
	for i, t := range p.CT {
		if present(t) {
			phases = append(phases, []string{"A", "B", "C"}[i])
		}
	}
	v["M13"] = strings.Join(phases, ", ")
	v["W14"] = `ООО "Газпром энерго"`
	v["Z14"] = "испытательная коробка"
	v["AB14"] = `пломба ООО "Газпром энерго"`
	v["W15"] = `ООО "Газпром энерго"`
	v["Z15"] = "крышка счетчика"
	v["AB15"] = `пломба ООО "Газпром энерго"`
	v["M20"] = "пломба"
	v["W21"] = "Установлено: измерительный комплекс учета электрической энергии может быть допущен к эксплуатации."
	v["Q21"] = "Схема соединения измерительных цепей"
	v["Q22"] = ""
	v["Z19"] = options.CompilationDate
	for i, m := range options.Commission {
		v[fmt.Sprintf("W%d", 27+i)] = m.Signature()
	}
	loads := EstimateBurden(p, options)
	for i := 0; i < 3; i++ {
		v[fmt.Sprintf("P%d", 29+6*i)] = loads[i]
		v[fmt.Sprintf("V%d", 5+6*i)] = loads[3+i]
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
		case "Лист2":
			w.Parts[s.Part] = burdenSheet(p, options)
			w.Parts["xl/workbook.xml"] = bytes.ReplaceAll(w.Parts["xl/workbook.xml"], []byte(`name="Лист2"`), []byte(`name="расчёт нагрузки"`))
			w.Parts["xl/workbook.xml"] = bytes.ReplaceAll(w.Parts["xl/workbook.xml"], []byte("Лист2!"), []byte("'расчёт нагрузки'!"))
			continue
		case "исх.данные":
			w.Parts[s.Part] = dataRE.ReplaceAllLiteral(w.Parts[s.Part], sourceData(p))
			continue
		default:
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
		if name == "xl/drawings/drawing1.xml" {
			data = fixedSchemeAnchors(data)
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
