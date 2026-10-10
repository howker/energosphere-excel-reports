package report

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"strconv"

	"github.com/howker/energosphere-excel-reports/internal/xlsx"
)

func phaseHeadingStyles(w *xlsx.Workbook, s *xlsx.Sheet, styles [][]byte) ([][]byte, map[string]int) {
	refs := []string{"K27", "K33", "K39", "Q3", "Q9", "Q15"}
	ids := map[string]int{}
	for _, ref := range refs {
		cell, ok := s.Cells[ref]
		if !ok || cell.Style >= len(styles) {
			continue
		}
		style := wrappingStyle(styles[cell.Style])
		style = alignmentRE.ReplaceAllLiteral(style, []byte(`<alignment horizontal="center" vertical="center" wrapText="1"/>`))
		ids[ref] = len(styles)
		styles = append(styles, style)
	}
	return styles, ids
}

// Масштаб по высоте А4 с запасом сохраняет ручные границы страниц.
// Режим fitToPage не используется: Excel игнорирует в нём эти границы.
func passportPrintSetup(w *xlsx.Workbook, data []byte, last int) []byte {
	data = regexp.MustCompile(`(?s)<rowBreaks\b[^>]*>.*?</rowBreaks>`).ReplaceAll(data, nil)
	data = rowRE.ReplaceAllFunc(data, func(b []byte) []byte {
		m := refRE.FindSubmatch(b)
		if len(m) < 2 {
			return b
		}
		n, _ := strconv.Atoi(string(m[1]))
		if n > last {
			return nil
		}
		return b
	})
	// Удаляем пустые ячейки за пределами AD: Excel учитывает их стили
	// в UsedRange и создаёт дополнительную пустую страницу.
	data = cellRE.ReplaceAllFunc(data, func(b []byte) []byte {
		m := refRE.FindSubmatch(b)
		if len(m) < 2 {
			return b
		}
		col, _ := xlsx.Coordinates(string(m[1]))
		if col > 30 {
			return nil
		}
		return b
	})
	height := 0.0
	for _, row := range rowRE.FindAll(data, -1) {
		h := 18.75
		m := regexp.MustCompile(` ht="([^"]+)"`).FindSubmatch(row)
		if len(m) == 2 {
			h, _ = strconv.ParseFloat(string(m[1]), 64)
		}
		height += h
	}
	// A4 841.89 пт; поля по 10 мм и запас 12 пт на разные драйверы.
	scale := int(math.Floor((841.89 - 2*0.39*72 - 12) * 100 / height))
	if scale > 90 {
		scale = 90
	}
	if scale < 10 {
		scale = 10
	}
	data = regexp.MustCompile(`<pageSetup\b[^>]*/>`).ReplaceAllFunc(data, func(old []byte) []byte {
		printer := regexp.MustCompile(` r:id="[^"]*"`).Find(old)
		return []byte(fmt.Sprintf(`<pageSetup paperSize="9" orientation="portrait" scale="%d" pageOrder="overThenDown"%s/>`, scale, printer))
	})
	data = regexp.MustCompile(`<pageMargins\b[^>]*/>`).ReplaceAllLiteral(data, []byte(`<pageMargins left="0.47" right="0.47" top="0.39" bottom="0.39" header="0.16" footer="0.16"/>`))
	if bytes.Contains(data, []byte("<printOptions")) {
		data = regexp.MustCompile(`<printOptions\b[^>]*/>`).ReplaceAllLiteral(data, []byte(`<printOptions horizontalCentered="1" gridLines="0" headings="0"/>`))
	} else {
		data = bytes.Replace(data, []byte("<pageMargins"), []byte(`<printOptions horizontalCentered="1" gridLines="0" headings="0"/><pageMargins`), 1)
	}
	setup := []byte(`<pageSetUpPr fitToPage="0" autoPageBreaks="0"/>`)
	if regexp.MustCompile(`<pageSetUpPr\b[^>]*/>`).Match(data) {
		data = regexp.MustCompile(`<pageSetUpPr\b[^>]*/>`).ReplaceAllLiteral(data, setup)
	} else if bytes.Contains(data, []byte("</sheetPr>")) {
		data = bytes.Replace(data, []byte("</sheetPr>"), append(setup, []byte("</sheetPr>")...), 1)
	} else {
		data = regexp.MustCompile(`<sheetPr\b[^>]*/>`).ReplaceAllFunc(data, func(b []byte) []byte {
			return []byte(string(bytes.TrimSuffix(b, []byte("/>"))) + ">" + string(setup) + "</sheetPr>")
		})
	}
	breaks := []byte(fmt.Sprintf(`<colBreaks count="3" manualBreakCount="3"><brk id="10" max="%d" man="1"/><brk id="16" max="%d" man="1"/><brk id="22" max="%d" man="1"/></colBreaks>`, last-1, last-1, last-1))
	if colBreaksRE.Match(data) {
		data = colBreaksRE.ReplaceAllLiteral(data, breaks)
	} else {
		data = bytes.Replace(data, []byte("<drawing"), append(breaks, []byte("<drawing")...), 1)
	}
	// Кавычки вокруг имени листа нужны для распознавания области старым Excel.
	areaRE := regexp.MustCompile(`(<definedName\b[^>]*name="_xlnm.Print_Area"[^>]*>)[^<]*(</definedName>)`)
	w.Parts["xl/workbook.xml"] = areaRE.ReplaceAllFunc(w.Parts["xl/workbook.xml"], func(b []byte) []byte {
		m := areaRE.FindSubmatch(b)
		return []byte(string(m[1]) + "'Лист1'!$A$1:$AD$" + strconv.Itoa(last) + string(m[2]))
	})
	return data
}
