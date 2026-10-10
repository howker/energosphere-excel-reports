package report

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/howker/energosphere-excel-reports/internal/passport"
	"github.com/howker/energosphere-excel-reports/internal/xlsx"
)

var xfsRE = regexp.MustCompile(`(?s)<cellXfs\b[^>]*>(.*?)</cellXfs>`)
var xfRE = regexp.MustCompile(`(?s)<xf\b[^>]*?(?:/>|>.*?</xf>)`)
var alignmentRE = regexp.MustCompile(`<alignment\b[^>]*/>`)
var wrapRE = regexp.MustCompile(` wrapText="[^"]*"`)
var applyAlignmentRE = regexp.MustCompile(` applyAlignment="[^"]*"`)
var rowRE = regexp.MustCompile(`(?s)<row\b[^>]*?(?:/>|>.*?</row>)`)
var heightRE = regexp.MustCompile(` (ht|customHeight)="[^"]*"`)
var mergesRE = regexp.MustCompile(`(?s)<mergeCells\b[^>]*>(.*?)</mergeCells>`)
var dimensionRE = regexp.MustCompile(`<dimension\b[^>]*/>`)
var colBreaksRE = regexp.MustCompile(`(?s)<colBreaks\b[^>]*>.*?</colBreaks>`)
var scaleRE = regexp.MustCompile(` scale="[^"]*"`)
var borderAttrRE = regexp.MustCompile(` borderId="[^"]*"`)

// wrappingStyle clones a style to enable text wrapping and vertical centering,
// strictly preserving fonts, borders, and horizontal alignment.
func wrappingStyle(old []byte) []byte {

	old = applyAlignmentRE.ReplaceAll(old, nil)
	end := bytes.IndexByte(old, '>')
	if end < 0 {
		return old
	}
	if bytes.HasSuffix(old, []byte("/>")) {
		return []byte(strings.TrimSuffix(string(old), "/>") + ` applyAlignment="1"><alignment wrapText="1" vertical="center"/></xf>`)
	}
	old = append(append(append([]byte{}, old[:end]...), []byte(` applyAlignment="1"`)...), old[end:]...)
	if alignmentRE.Match(old) {
		return alignmentRE.ReplaceAllFunc(old, func(b []byte) []byte {
			b = wrapRE.ReplaceAll(b, nil)
			return bytes.Replace(b, []byte("/>"), []byte(` wrapText="1"/>`), 1)
		})
	}
	return bytes.Replace(old, []byte("</xf>"), []byte(`<alignment wrapText="1" vertical="center"/></xf>`), 1)
}

// closedBorderStyle ensures full outer border (borderId=2) for table edge cells (column V in VT table)
func closedBorderStyle(old []byte) []byte {
	s := wrappingStyle(old)
	if bytes.Contains(s, []byte(`borderId="`)) {
		s = borderAttrRE.ReplaceAllLiteral(s, []byte(` borderId="2"`))
	} else {
		end := bytes.IndexByte(s, '>')
		if end > 0 {
			s = append(append(append([]byte{}, s[:end]...), []byte(` borderId="2" applyBorder="1"`)...), s[end:]...)
		}
	}
	s = alignmentRE.ReplaceAllLiteral(s, []byte(`<alignment horizontal="center" vertical="center" wrapText="1"/>`))
	return s
}
func layout(w *xlsx.Workbook, s *xlsx.Sheet, values map[string]string, options passport.Options) error {
	refs := strings.Fields("K1 K2 Q1 Q2 R2 M3 M4 P4 M5 P5 M6 P6 M7 P7 M9 P9 M10 P10 M12 P12 M13 P13 M15 P15 A9 A18 K25 K26 Q21 Q22 W5 Y5 AA5 AC5 W6 Y6 AA6 AC6 W11 W14 Z14 AB14 W15 Z15 AB15 W19 Z19 W21")
	for i := 0; i < 3; i++ {
		ct := 28 + 6*i
		refs = append(refs, fmt.Sprintf("L%d", ct), fmt.Sprintf("N%d", ct), fmt.Sprintf("P%d", ct))
		refs = append(refs, fmt.Sprintf("L%d", ct+1), fmt.Sprintf("N%d", ct+1), fmt.Sprintf("P%d", ct+1))
		refs = append(refs, fmt.Sprintf("L%d", ct+3), fmt.Sprintf("N%d", ct+3), fmt.Sprintf("P%d", ct+3))

		vt := 4 + 6*i
		refs = append(refs, fmt.Sprintf("R%d", vt), fmt.Sprintf("T%d", vt), fmt.Sprintf("V%d", vt))
		refs = append(refs, fmt.Sprintf("R%d", vt+1), fmt.Sprintf("T%d", vt+1), fmt.Sprintf("V%d", vt+1))
		refs = append(refs, fmt.Sprintf("R%d", vt+3), fmt.Sprintf("T%d", vt+3), fmt.Sprintf("V%d", vt+3))
	}
	merges := []string{"K1:P1", "K2:P2", "Q1:V1", "R2:V2", "M3:P3", "K25:P25", "K26:P26", "W19:Y19", "Z19:AD19", "Q21:V21", "W21:AD22"}
	last := 44
	for i := range options.Commission {
		n := 27 + i
		refs = append(refs, fmt.Sprintf("W%d", n))
		merges = append(merges, fmt.Sprintf("W%d:AD%d", n, n))
		if n > last {
			last = n
		}
	}
	xfs := xfsRE.FindSubmatch(w.Parts["xl/styles.xml"])
	if len(xfs) < 2 {
		return fmt.Errorf("не найдены стили шаблона")
	}
	baseStyles := xfRE.FindAll(xfs[1], -1)
	styles := append([][]byte{}, baseStyles...)
	clones := map[int]int{}
	cellStyles := map[string]int{}
	for _, ref := range refs {
		c, ok := s.Cells[ref]
		if ref == "R2" {
			if m3, hasM3 := s.Cells["M3"]; hasM3 {
				c = m3
				ok = true
			}
		}
		if ref == "Q22" {
			if q21, hasQ21 := s.Cells["Q21"]; hasQ21 {
				c = q21
				ok = true
			}
		}
		if ref == "W15" || ref == "W16" || ref == "W17" {
			if w14, hasW14 := s.Cells["W14"]; hasW14 {
				c = w14
				ok = true
			}
		}
		if ref == "Z15" || ref == "Z16" || ref == "Z17" {
			if z14, hasZ14 := s.Cells["Z14"]; hasZ14 {
				c = z14
				ok = true
			}
		}
		if ref == "AB15" || ref == "AB16" || ref == "AB17" {
			if ab14, hasAB14 := s.Cells["AB14"]; hasAB14 {
				c = ab14
				ok = true
			}
		}
		if !ok {
			c = s.Cells["W27"]
		}
		if c.Style < 0 || c.Style >= len(baseStyles) {
			return fmt.Errorf("неверный стиль %s", ref)
		}
		id, ok := clones[c.Style]
		if !ok {
			id = len(styles)
			styles = append(styles, wrappingStyle(baseStyles[c.Style]))
			clones[c.Style] = id
		}
		cellStyles[ref] = id
	}
	// Ensure right borders are closed on column V of VT table (rows 3 to 20)
	closedClones := map[int]int{}
	for r := 3; r <= 20; r++ {
		ref := fmt.Sprintf("V%d", r)
		c, ok := s.Cells[ref]
		if !ok || c.Style < 0 || c.Style >= len(baseStyles) {
			continue
		}
		id, ok := closedClones[c.Style]
		if !ok {
			id = len(styles)
			styles = append(styles, closedBorderStyle(baseStyles[c.Style]))
			closedClones[c.Style] = id
		}
		cellStyles[ref] = id
	}

	if len(options.Commission) > 0 {
		heading, ok := s.Cells["W25"]
		if !ok || heading.Style < 0 || heading.Style >= len(baseStyles) {
			return fmt.Errorf("не найден стиль заголовка комиссии")
		}
		id := len(styles)
		style := wrappingStyle(baseStyles[heading.Style])
		style = alignmentRE.ReplaceAllLiteral(style, []byte(`<alignment horizontal="left" vertical="center" wrapText="1"/>`))
		styles = append(styles, style)
		for i := range options.Commission {
			cellStyles[fmt.Sprintf("W%d", 27+i)] = id
		}
	}
	for _, ref := range []string{"W19", "Z19"} {
		id := len(styles)
		style := wrappingStyle(baseStyles[s.Cells[ref].Style])
		align := "left"
		if ref == "W19" {
			align = "right"
		}
		style = alignmentRE.ReplaceAllLiteral(style, []byte(`<alignment horizontal="`+align+`" vertical="center" wrapText="1"/>`))
		styles = append(styles, style)
		cellStyles[ref] = id
	}
	styles, phaseStyles := phaseHeadingStyles(w, s, styles)
	for ref, id := range phaseStyles {
		cellStyles[ref] = id
	}
	styleBlock := []byte(fmt.Sprintf(`<cellXfs count="%d">`, len(styles)))
	for _, style := range styles {
		styleBlock = append(styleBlock, style...)
	}
	styleBlock = append(styleBlock, []byte(`</cellXfs>`)...)
	w.Parts["xl/styles.xml"] = xfsRE.ReplaceAllLiteral(w.Parts["xl/styles.xml"], styleBlock)
	data := w.Parts[s.Part]
	data = cellRE.ReplaceAllFunc(data, func(b []byte) []byte {
		ref := refRE.FindSubmatch(b)
		if len(ref) == 2 {
			if id, ok := cellStyles[string(ref[1])]; ok {
				return []byte(textCell(string(ref[1]), strconv.Itoa(id), values[string(ref[1])]))
			}
		}
		return b
	})
	heights := map[int]float64{
		1: 24, 2: math.Max(26, lines(values["R2"], 50)*18),
		3: math.Max(30, lines(values["M3"], 55)*18), 5: math.Max(54, lines(values["M5"], 12)*18),
		9: 26, 11: 34, 14: 36, 18: math.Max(36, lines(values["A18"], 70)*18), 19: 22,
		21: 24, 22: 18,
		25: 24, 26: math.Max(26, lines(values["K26"], 65)*18),
	}
	for i := 0; i < 3; i++ {
		ct, vt := 28+6*i, 4+6*i
		heights[ct] = math.Max(36, lines(values[fmt.Sprintf("L%d", ct)], 10)*18)
		heights[vt] = math.Max(36, lines(values[fmt.Sprintf("R%d", vt)], 10)*18)
		heights[ct-1] = 22
		heights[vt-1] = 22
	}
	for i, m := range options.Commission {
		heights[27+i] = math.Max(heights[27+i], math.Max(24, lines(m.Signature(), 65)*18))
	}
	seenRows := map[int]bool{}
	seenCells := map[string]bool{}
	data = rowRE.ReplaceAllFunc(data, func(b []byte) []byte {
		match := refRE.FindSubmatch(b)
		if len(match) < 2 {
			return b
		}
		row, e := strconv.Atoi(string(match[1]))
		if e != nil {
			return b
		}
		seenRows[row] = true
		for _, c := range cellRE.FindAll(b, -1) {
			r := refRE.FindSubmatch(c)
			if len(r) == 2 {
				seenCells[string(r[1])] = true
			}
		}
		for ref, id := range cellStyles {
			_, n := xlsx.Coordinates(ref)
			if n == row && !seenCells[ref] {
				b = bytes.Replace(b, []byte("</row>"), []byte(textCell(ref, strconv.Itoa(id), values[ref])+"</row>"), 1)
			}
		}
		// Excel requires cell addresses in ascending column order. New fields
		// such as Z19 may fall between existing blank styled cells.
		cells := cellRE.FindAll(b, -1)
		sort.Slice(cells, func(i, j int) bool {
			a, _ := xlsx.Coordinates(string(refRE.FindSubmatch(cells[i])[1]))
			c, _ := xlsx.Coordinates(string(refRE.FindSubmatch(cells[j])[1]))
			return a < c
		})
		i := 0
		b = cellRE.ReplaceAllFunc(b, func([]byte) []byte { cell := cells[i]; i++; return cell })
		if h, ok := heights[row]; ok {
			end := bytes.IndexByte(b, '>')
			head := heightRE.ReplaceAll(b[:end], nil)
			head = append(head, []byte(fmt.Sprintf(` ht="%.2f" customHeight="1"`, h))...)
			b = append(append(head, '>'), b[end+1:]...)
		}
		return b
	})
	newRows := []int{}
	for n := range heights {
		if !seenRows[n] {
			newRows = append(newRows, n)
		}
	}
	sort.Ints(newRows)
	for _, n := range newRows {
		ref := fmt.Sprintf("W%d", n)
		if id, ok := cellStyles[ref]; ok {
			row := fmt.Sprintf(`<row r="%d" ht="%.2f" customHeight="1">%s</row>`, n, heights[n], textCell(ref, strconv.Itoa(id), values[ref]))
			data = bytes.Replace(data, []byte("</sheetData>"), []byte(row+"</sheetData>"), 1)
		}
	}
	// All added ranges occupy formerly blank cells, never source values.
	data = mergesRE.ReplaceAllFunc(data, func(b []byte) []byte {
		inner := mergesRE.FindSubmatch(b)[1]
		inner = bytes.ReplaceAll(inner, []byte(`<mergeCell ref="Q22:V22"/>`), nil)
		inner = bytes.ReplaceAll(inner, []byte(`<mergeCell ref="Q41:V42"/>`), nil)
		count := bytes.Count(inner, []byte("<mergeCell "))
		for _, r := range merges {
			if !bytes.Contains(inner, []byte(`ref="`+r+`"`)) {
				inner = append(inner, []byte(`<mergeCell ref="`+r+`"/>`)...)
				count++
			}
		}
		return []byte(fmt.Sprintf(`<mergeCells count="%d">%s</mergeCells>`, count, inner))
	})
	// Remove stray vertical borders on Page 1 (J7, J16, J18) and Page 3 (Q41, Q42)
	cleanStyle := "75"
	if noBorderCell, ok := s.Cells["J8"]; ok && noBorderCell.Style >= 0 {
		cleanStyle = strconv.Itoa(noBorderCell.Style)
		for _, stray := range []string{"J7", "J16", "J18", "Q41", "Q42"} {
			re := regexp.MustCompile(fmt.Sprintf(`(<c r="%s" s=")[^"]+(")`, stray))
			data = re.ReplaceAll(data, []byte("${1}"+cleanStyle+"${2}"))
		}
	}

	// Optimize column widths across all 4 pages (~105-109 units per page, perfectly fitting A4 portrait)
	colsBlock := []byte(`<cols>` +
		`<col min="1" max="10" width="9.14" style="1"/>` +
		`<col min="11" max="11" width="17.0" style="1" customWidth="1"/>` +
		`<col min="12" max="12" width="17.7" style="1" customWidth="1"/>` +
		`<col min="13" max="13" width="17.1" style="1" customWidth="1"/>` +
		`<col min="14" max="14" width="17.3" style="1" customWidth="1"/>` +
		`<col min="15" max="15" width="18.9" style="1" customWidth="1"/>` +
		`<col min="16" max="16" width="17.4" style="1" customWidth="1"/>` +
		`<col min="17" max="17" width="17.5" style="1" customWidth="1"/>` +
		`<col min="18" max="18" width="18.8" style="1" customWidth="1"/>` +
		`<col min="19" max="19" width="16.7" style="1" customWidth="1"/>` +
		`<col min="20" max="20" width="15.0" style="1" customWidth="1"/>` +
		`<col min="21" max="21" width="18.6" style="1" customWidth="1"/>` +
		`<col min="22" max="22" width="18.1" style="1" customWidth="1"/>` +
		`<col min="23" max="23" width="18.7" style="1" customWidth="1"/>` +
		`<col min="24" max="24" width="13.6" style="1" customWidth="1"/>` +
		`<col min="25" max="25" width="9.14" style="1"/>` +
		`<col min="26" max="26" width="17.3" style="1" customWidth="1"/>` +
		`<col min="27" max="27" width="15.9" style="1" customWidth="1"/>` +
		`<col min="28" max="28" width="15.7" style="1" customWidth="1"/>` +
		`<col min="29" max="29" width="9.6" style="1" customWidth="1"/>` +
		`<col min="30" max="16384" width="9.14" style="1"/>` +
		`</cols>`)
	data = regexp.MustCompile(`(?s)<cols\b[^>]*>.*?</cols>`).ReplaceAllLiteral(data, colsBlock)

	data = passportPrintSetup(w, data, last)
	data = dimensionRE.ReplaceAll(data, []byte(fmt.Sprintf(`<dimension ref="A1:AD%d"/>`, last)))
	data, err := normalFormFonts(w, data)
	if err != nil {
		return err
	}
	w.Parts[s.Part] = data
	if last > 44 {
		w.Parts["xl/workbook.xml"] = bytes.ReplaceAll(w.Parts["xl/workbook.xml"], []byte("$AD$44"), []byte(fmt.Sprintf("$AD$%d", last)))
	}
	return nil
}
func lines(s string, width int) float64 {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		n += int(math.Ceil(float64(utf8.RuneCountInString(line)) / float64(width)))
	}
	if n < 1 {
		n = 1
	}
	return float64(n)
}
