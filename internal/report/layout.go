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

// Clone styles for the changed fields only; keep fonts, borders and alignment.
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
func layout(w *xlsx.Workbook, s *xlsx.Sheet, values map[string]string, options passport.Options) error {
	refs := strings.Fields("K1 K2 Q1 M3 M5 A9 A18 K25 K26 W11 W14 Z14 AB14 W19 Z19")
	for i := 0; i < 3; i++ {
		refs = append(refs, fmt.Sprintf("L%d", 28+6*i), fmt.Sprintf("N%d", 28+6*i), fmt.Sprintf("R%d", 4+6*i), fmt.Sprintf("T%d", 4+6*i))
	}
	merges := []string{"K1:P1", "K2:P2", "Q1:V1", "M3:P3", "K25:N25", "K26:P26", "W19:Y19", "Z19:AD19"}
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
	// Heights account for wrapping in the narrow meter type field. Others are
	// wider merged fields. Keep the source font instead of shrinking the text.
	heights := map[int]float64{1: 22, 2: 22, 3: math.Max(36, lines(values["M3"], 55)*18), 5: math.Max(60, lines(values["M5"], 13)*18), 9: 36, 11: 48, 14: 54, 18: math.Max(36, lines(values["A18"], 70)*18), 19: 22, 25: 24, 26: math.Max(24, lines(values["K26"], 80)*18)}
	for i := 0; i < 3; i++ {
		ct, vt := 28+6*i, 4+6*i
		heights[ct] = math.Max(24, lines(values[fmt.Sprintf("L%d", ct)], 12)*18)
		heights[vt] = math.Max(24, lines(values[fmt.Sprintf("R%d", vt)], 16)*18)
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
		count := bytes.Count(inner, []byte("<mergeCell "))
		for _, r := range merges {
			if !bytes.Contains(inner, []byte(`ref="`+r+`"`)) {
				inner = append(inner, []byte(`<mergeCell ref="`+r+`"/>`)...)
				count++
			}
		}
		return []byte(fmt.Sprintf(`<mergeCells count="%d">%s</mergeCells>`, count, inner))
	})
	data = dimensionRE.ReplaceAll(data, []byte(fmt.Sprintf(`<dimension ref="A1:AD%d"/>`, last)))
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
