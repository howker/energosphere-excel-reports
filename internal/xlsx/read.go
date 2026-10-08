// Package xlsx implements the small OOXML subset used by the supplied sources.
// It requires neither Excel automation nor third-party dependencies.
package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"
)

const maxPart = 64 << 20

type TextRun struct {
	Text string `xml:"t"`
}
type RichText struct {
	Text string    `xml:"t"`
	Runs []TextRun `xml:"r"`
}

func (r RichText) String() string {
	s := r.Text
	for _, run := range r.Runs {
		s += run.Text
	}
	return strings.ReplaceAll(s, "_x000D_", "\r")
}

type Cell struct {
	Ref     string   `xml:"r,attr"`
	Type    string   `xml:"t,attr"`
	Style   int      `xml:"s,attr"`
	Value   string   `xml:"v"`
	Inline  RichText `xml:"is"`
	Formula string   `xml:"f"`
}
type Row struct {
	Number int    `xml:"r,attr"`
	Cells  []Cell `xml:"c"`
}
type Merge struct {
	Ref string `xml:"ref,attr"`
}
type Sheet struct {
	Rows       []Row           `xml:"sheetData>row"`
	Merges     []Merge         `xml:"mergeCells>mergeCell"`
	Cells      map[string]Cell `xml:"-"`
	Name, Part string          `xml:"-"`
	ranges     []cellRange
	byRow      map[int][]cellRange
}
type cellRange struct {
	left, top, right, bottom int
	anchor                   string
}
type sheetRef struct {
	Name string `xml:"name,attr"`
	ID   string `xml:"id,attr"`
}
type Workbook struct {
	Parts    map[string][]byte
	Sheets   []*Sheet
	Shared   []RichText
	Formats  []string
	Date1904 bool
}

func Open(filename string) (*Workbook, error) {
	return OpenWithProgress(filename, nil)
}
func OpenWithProgress(filename string, notify func(int, string)) (*Workbook, error) {
	z, err := zip.OpenReader(filename)
	if err != nil {
		return nil, fmt.Errorf("открытие XLSX: %w", err)
	}
	defer z.Close()
	return read(z.File, notify)
}
func FromBytes(data []byte) (*Workbook, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	return read(z.File, nil)
}
func read(files []*zip.File, notify func(int, string)) (*Workbook, error) {
	progress := func(n int, stage string) {
		if notify != nil {
			notify(n, stage)
		}
	}
	progress(0, "Открытие книги")
	w := &Workbook{Parts: make(map[string][]byte)}
	var total int64
	for i, f := range files {
		if f.UncompressedSize64 > maxPart {
			return nil, fmt.Errorf("слишком большой раздел XLSX: %s", f.Name)
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(r, maxPart+1))
		r.Close()
		if err != nil {
			return nil, err
		}
		total += int64(len(b))
		if len(b) > maxPart || total > 128<<20 {
			return nil, fmt.Errorf("книга превышает лимит 128 МБ распакованных данных")
		}
		w.Parts[f.Name] = b
		progress((i+1)*25/len(files), "Чтение разделов XLSX")
	}
	var wb struct {
		Sheets []sheetRef `xml:"sheets>sheet"`
		Props  struct {
			Date1904 string `xml:"date1904,attr"`
		} `xml:"workbookPr"`
	}
	if err := xml.Unmarshal(w.Parts["xl/workbook.xml"], &wb); err != nil {
		return nil, fmt.Errorf("workbook.xml: %w", err)
	}
	w.Date1904 = wb.Props.Date1904 == "1" || wb.Props.Date1904 == "true"
	var rels struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(w.Parts["xl/_rels/workbook.xml.rels"], &rels); err != nil {
		return nil, err
	}
	targets := make(map[string]string)
	for _, r := range rels.Items {
		t := path.Clean(path.Join("xl", r.Target))
		if strings.HasPrefix(r.Target, "/") {
			t = strings.TrimPrefix(r.Target, "/")
		}
		targets[r.ID] = t
	}
	if b := w.Parts["xl/sharedStrings.xml"]; len(b) > 0 {
		progress(28, "Чтение строк книги")
		var ss struct {
			Items []RichText `xml:"si"`
		}
		if err := xml.Unmarshal(b, &ss); err != nil {
			return nil, err
		}
		w.Shared = ss.Items
	}
	var styles struct {
		Custom []struct {
			ID   int    `xml:"numFmtId,attr"`
			Code string `xml:"formatCode,attr"`
		} `xml:"numFmts>numFmt"`
		Xfs []struct {
			ID int `xml:"numFmtId,attr"`
		} `xml:"cellXfs>xf"`
	}
	if err := xml.Unmarshal(w.Parts["xl/styles.xml"], &styles); err != nil && len(w.Parts["xl/styles.xml"]) > 0 {
		return nil, err
	}
	codes := map[int]string{14: "mm-dd-yy", 15: "d-mmm-yy", 16: "d-mmm", 17: "mmm-yy", 22: "m/d/yy h:mm", 49: "@"}
	for _, f := range styles.Custom {
		codes[f.ID] = f.Code
	}
	for _, f := range styles.Xfs {
		w.Formats = append(w.Formats, codes[f.ID])
	}
	for i, ref := range wb.Sheets {
		progress(35+i*25/len(wb.Sheets), "Чтение листа: "+ref.Name)
		s := &Sheet{Name: ref.Name, Part: targets[ref.ID], Cells: make(map[string]Cell)}
		if err := xml.Unmarshal(w.Parts[s.Part], s); err != nil {
			return nil, fmt.Errorf("лист %s: %w", ref.Name, err)
		}
		for _, r := range s.Rows {
			for _, c := range r.Cells {
				s.Cells[c.Ref] = c
			}
		}
		w.Sheets = append(w.Sheets, s)
		s.byRow = make(map[int][]cellRange)
		for _, m := range s.Merges {
			p := strings.Split(m.Ref, ":")
			if len(p) == 2 {
				a, b := Coordinates(p[0])
				c, d := Coordinates(p[1])
				s.ranges = append(s.ranges, cellRange{a, b, c, d, p[0]})
			}
		}
	}
	progress(60, "Листы прочитаны")
	return w, nil
}
func (w *Workbook) Raw(c Cell) (string, error) {
	s := c.Value
	switch c.Type {
	case "s":
		i, err := strconv.Atoi(s)
		if err != nil || i < 0 || i >= len(w.Shared) {
			return "", fmt.Errorf("неверная строка %s", c.Ref)
		}
		s = w.Shared[i].String()
	case "inlineStr":
		s = c.Inline.String()
	case "e":
		return "", fmt.Errorf("ячейка %s содержит ошибку Excel: %s", c.Ref, s)
	}
	return strings.TrimSpace(strings.ReplaceAll(s, "_x000D_", "\r")), nil
}
func (w *Workbook) Text(c Cell, date bool) (string, error) {
	s, err := w.Raw(c)
	if err != nil || s == "" {
		return s, err
	}
	if c.Type == "s" || c.Type == "inlineStr" || c.Type == "str" {
		return s, nil
	}
	if c.Type == "d" {
		if len(s) >= 10 {
			if t, e := time.Parse("2006-01-02", s[:10]); e == nil {
				return t.Format("02.01.2006"), nil
			}
		}
		return s, nil
	}
	if date {
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return s, nil
		}
		base := time.Date(1899, 12, 31, 0, 0, 0, 0, time.UTC)
		days := int(n)
		// A zero/time-only value in a 1900 date column is not a calendar
		// date. Preserve its source number instead of inventing 1899-12-31.
		if !w.Date1904 && n >= 0 && n < 1 {
			return s, nil
		}
		if !w.Date1904 && days == 60 {
			return "29.02.1900", nil
		}
		if w.Date1904 {
			base = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
		} else if days >= 60 {
			days--
		}
		if days < 0 || days > 3000000 {
			return "", fmt.Errorf("неверная дата в %s", c.Ref)
		}
		return base.AddDate(0, 0, days).Format("02.01.2006"), nil
	}
	// A numeric identifier formatted as 000000 must keep those zeroes too.
	if c.Style >= 0 && c.Style < len(w.Formats) {
		f := w.Formats[c.Style]
		if len(f) > 1 && strings.Trim(f, "0") == "" {
			n, e := strconv.ParseInt(s, 10, 64)
			if e == nil {
				return fmt.Sprintf("%0*d", len(f), n), nil
			}
		}
	}
	return s, nil
}
func Coordinates(ref string) (col, row int) {
	for _, r := range strings.ReplaceAll(ref, "$", "") {
		if r >= 'A' && r <= 'Z' {
			col = col*26 + int(r-'A'+1)
		} else if r >= '0' && r <= '9' {
			row = row*10 + int(r-'0')
		}
	}
	return
}
func Column(n int) string {
	s := ""
	for n > 0 {
		n--
		s = string(rune('A'+n%26)) + s
		n /= 26
	}
	return s
}
func (s *Sheet) At(col, row int) Cell {
	ref := Column(col) + strconv.Itoa(row)
	ranges, ok := s.byRow[row]
	if !ok {
		for _, r := range s.ranges {
			if row >= r.top && row <= r.bottom {
				ranges = append(ranges, r)
			}
		}
		s.byRow[row] = ranges
	}
	for _, r := range ranges {
		if col >= r.left && col <= r.right {
			ref = r.anchor
			break
		}
	}
	return s.Cells[ref]
}
