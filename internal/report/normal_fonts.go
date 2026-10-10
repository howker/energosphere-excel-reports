package report

import (
	"bytes"
	"fmt"
	"github.com/howker/energosphere-excel-reports/internal/xlsx"
	"regexp"
	"strconv"
)

var fontsRE = regexp.MustCompile(`(?s)<fonts\b[^>]*>(.*?)</fonts>`)
var fontRE = regexp.MustCompile(`(?s)<font\b[^>]*?(?:/>|>.*?</font>)`)
var boldRE = regexp.MustCompile(`(?s)<b\b[^>]*?(?:/>|>.*?</b>)`)
var fontIDRE = regexp.MustCompile(`\bfontId="([0-9]+)"`)

// Retain the first page's styles; use normal font variants in columns K:AD.
func normalFormFonts(w *xlsx.Workbook, data []byte) ([]byte, error) {
	original := w.Parts["xl/styles.xml"]
	fm := fontsRE.FindSubmatch(original)
	xm := xfsRE.FindSubmatch(original)
	if len(fm) < 2 || len(xm) < 2 {
		return nil, fmt.Errorf("не найдены шрифты формы")
	}
	fonts := fontRE.FindAll(fm[1], -1)
	styles := xfRE.FindAll(xm[1], -1)
	normalFonts := map[int]int{}
	for i, f := range fonts {
		if boldRE.Match(f) {
			normalFonts[i] = len(fonts)
			fonts = append(fonts, boldRE.ReplaceAll(f, nil))
		}
	}
	clones := map[int]int{}
	var failure error
	data = cellRE.ReplaceAllFunc(data, func(b []byte) []byte {
		r := refRE.FindSubmatch(b)
		if len(r) < 2 {
			return b
		}
		col, _ := xlsx.Coordinates(string(r[1]))
		if col < 11 {
			return b
		}
		st := styleRE.FindSubmatch(b)
		if len(st) < 2 {
			return b
		}
		id, _ := strconv.Atoi(string(st[1]))
		if id < 0 || id >= len(styles) {
			failure = fmt.Errorf("неверный стиль %s", r[1])
			return b
		}
		f := fontIDRE.FindSubmatch(styles[id])
		if len(f) < 2 {
			return b
		}
		font, _ := strconv.Atoi(string(f[1]))
		newFont, ok := normalFonts[font]
		if !ok {
			return b
		}
		newStyle, ok := clones[id]
		if !ok {
			newStyle = len(styles)
			styles = append(styles, fontIDRE.ReplaceAllLiteral(styles[id], []byte(fmt.Sprintf(`fontId="%d"`, newFont))))
			clones[id] = newStyle
		}
		return styleRE.ReplaceAllLiteral(b, []byte(fmt.Sprintf(`s="%d"`, newStyle)))
	})
	if failure != nil {
		return nil, failure
	}
	fb := []byte(fmt.Sprintf(`<fonts count="%d">`, len(fonts)))
	for _, f := range fonts {
		fb = append(fb, f...)
	}
	fb = append(fb, []byte(`</fonts>`)...)
	xb := []byte(fmt.Sprintf(`<cellXfs count="%d">`, len(styles)))
	for _, s := range styles {
		xb = append(xb, s...)
	}
	xb = append(xb, []byte(`</cellXfs>`)...)
	original = fontsRE.ReplaceAllLiteral(original, fb)
	w.Parts["xl/styles.xml"] = xfsRE.ReplaceAllLiteral(original, xb)
	return bytes.Clone(data), nil
}
