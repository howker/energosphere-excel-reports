package importer

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/howker/energosphere-excel-reports/internal/passport"
	"github.com/howker/energosphere-excel-reports/internal/xlsx"
)

func Load(filename string) ([]passport.Passport, error) {
	w, err := xlsx.Open(filename)
	if err != nil {
		return nil, err
	}
	var chosen *xlsx.Sheet
	for _, s := range w.Sheets {
		// Match column roles, not the sheet title, so renamed sheets still work.
		text := ""
		for _, r := range s.Rows {
			if r.Number > 9 {
				break
			}
			for _, c := range r.Cells {
				v, _ := w.Raw(c)
				text += strings.ToLower(v) + " "
			}
		}
		if strings.Contains(text, "трансформатор тока") && strings.Contains(text, "трансформатор напряжения") {
			b, _ := w.Raw(s.At(2, 6))
			e, _ := w.Raw(s.At(5, 6))
			h, _ := w.Raw(s.At(8, 6))
			if strings.Contains(strings.ToLower(b), "общество") && strings.Contains(strings.ToLower(e), "присоединения") && strings.Contains(strings.ToLower(h), "счетчик") {
				if chosen != nil {
					return nil, fmt.Errorf("в книге несколько подходящих листов")
				}
				chosen = s
			}
		}
	}
	if chosen == nil {
		return nil, fmt.Errorf("не найден лист формата Прил.1.1 (Сч,ТТ,ТН): ожидаются столбцы A:AF и заголовки в строках 6–8")
	}
	var out []passport.Passport
	for _, r := range chosen.Rows {
		if r.Number < 10 {
			continue
		}
		a, err := w.Raw(chosen.Cells["A"+strconv.Itoa(r.Number)])
		if err != nil {
			return nil, err
		}
		if _, err := strconv.Atoi(a); err != nil {
			continue
		}
		e := chosen.Cells["E"+strconv.Itoa(r.Number)]
		name, err := w.Raw(e)
		if err != nil {
			return nil, err
		}
		if name == "" {
			continue
		}
		end := r.Number
		for _, m := range chosen.Merges {
			p := strings.Split(m.Ref, ":")
			if len(p) == 2 && p[0] == e.Ref {
				_, end = xlsx.Coordinates(p[1])
				break
			}
		}
		if end != r.Number+2 {
			return nil, fmt.Errorf("строка %d: ожидается блок присоединения из трёх строк", r.Number)
		}
		p := passport.Passport{Row: r.Number}
		for i := 0; i < 3; i++ {
			for j := 0; j < 32; j++ {
				date := j == 11 || j == 12 || j == 19 || j == 20 || j == 27 || j == 28
				value, err := w.Text(chosen.At(j+1, r.Number+i), date)
				if err != nil {
					return nil, fmt.Errorf("строка %d: %w", r.Number+i, err)
				}
				p.SourceCells[i][j] = value
			}
		}
		v := p.SourceCells[0]
		p.Company, p.Object, p.Inventory, p.Connection, p.Voltage, p.Accounting = v[1], v[2], v[3], v[4], v[5], v[6]
		p.Exchange, p.Ownership = v[30], v[31]
		p.Meter = passport.Equipment{Type: v[7], Serial: v[8], Accuracy: v[9], Year: v[10], Verification: v[11], NextVerification: v[12], Certificate: v[13]}
		for i := 0; i < 3; i++ {
			v = p.SourceCells[i]
			p.CT[i] = equipment(v, 14)
			p.VT[i] = equipment(v, 22)
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("в книге нет присоединений")
	}
	return out, nil
}
func equipment(v [32]string, i int) passport.Equipment {
	return passport.Equipment{Type: v[i], Ratio: v[i+1], Serial: v[i+2], Accuracy: v[i+3], Year: v[i+4], Verification: v[i+5], NextVerification: v[i+6], Certificate: v[i+7]}
}
