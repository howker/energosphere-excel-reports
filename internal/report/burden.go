package report

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"github.com/howker/energosphere-excel-reports/internal/passport"
	"github.com/howker/energosphere-excel-reports/internal/xlsx"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const BurdenCatalogName = "Справочник_нагрузок.csv"
const burdenConditions = "Условный расчёт: один счётчик; медь, 20 м в одну сторону, 4 мм², 20 °C; двухпроводная петля; контакты 0,02 Ом. Прочие приборы и общая нагрузка нескольких присоединений на ТН не учтены."
const mercurySource = "https://doc.incotexcom.ru/hardware/204/"
const setSource = "https://nzif.ru/uploads/sel/set4tm03/rpe.pdf"
const setMSource = "https://nzif.ru/uploads/katalog/buklet2013.pdf"
const alphaSource = "https://cdn.uralenergotel.ru/uploads/products/elstersolutions/docs/A1800%20%D0%A0%D1%83%D0%BA%D0%BE%D0%B2%D0%BE%D0%B4%D1%81%D1%82%D0%B2%D0%BE%20%D0%BF%D0%BE%20%D1%8D%D0%BA%D1%81%D0%BF%D0%BB%D1%83%D0%B0%D1%82%D0%B0%D1%86%D0%B8%D0%B8.pdf"

func BurdenKey(p passport.Passport) string {
	return strings.Join([]string{p.Company, p.Object, p.Connection, p.Meter.Serial, p.Meter.Type}, "\x1f")
}
func meterBurden(m string) (float64, float64, string) {
	n := strings.ToLower(strings.TrimSpace(m))
	switch {
	case strings.Contains(n, "меркурий 234"):
		u := 9.0
		if strings.Contains(n, "-00") || strings.Contains(n, "-04") || strings.Contains(n, "-06") {
			u = 2
		}
		note := mercurySource
		if strings.Contains(n, ".g") {
			u = 10
			note += "; 30 ВА с модемом условно распределены поровну между 3 фазами"
		}
		return .1, u, note
	case strings.Contains(n, "сэт-4тм.03м"):
		return .1, 1, setMSource
	case strings.Contains(n, "сэт-4тм.03"):
		return .1, 1.5, setSource
	case strings.Contains(n, "a180") || strings.Contains(n, "а180"):
		return .003, 3.6, alphaSource + "; предельная мощность напряжения принята на фазу для консервативной оценки"
	default:
		return .1, 4, "Условный профиль без подтверждённых характеристик этой модификации: 0,1 ВА по току, 4 ВА по напряжению на фазу; требует уточнения по паспорту прибора"
	}
}
func secondaryCurrent(t passport.Equipment) float64 {
	parts := strings.Split(strings.ReplaceAll(t.Ratio, ",", "."), "/")
	if len(parts) > 1 {
		v, e := strconv.ParseFloat(strings.TrimSpace(parts[len(parts)-1]), 64)
		if e == nil && v > 0 && v <= 10 {
			return v
		}
	}
	return 5
}
func decimal(v float64) string {
	return strings.ReplaceAll(fmt.Sprintf("%.2f", math.Round((v+1e-9)*100)/100), ".", ",")
}
func EstimateBurden(p passport.Passport, options passport.Options) [6]string {
	if values, ok := options.BurdenOverrides[BurdenKey(p)]; ok {
		return values
	}
	var values [6]string
	if !present(p.Meter) {
		return values
	}
	si, su, _ := meterBurden(p.Meter.Type)
	// Scalar addition is a conservative upper estimate for apparent power.
	r := .0175*2*20/4 + .02
	for i := 0; i < 3; i++ {
		if present(p.CT[i]) {
			current := secondaryCurrent(p.CT[i])
			values[i] = decimal(si + current*current*r)
		}
		if present(p.VT[i]) {
			current := su / 57.7
			values[3+i] = decimal(su + current*current*r)
		}
	}
	return values
}

// A persistent per-connection catalog. Edits to its six VA columns override
// defaults on the next batch. Exact equipment identity avoids stale reuse.
func PrepareBurdenCatalog(filename string, items []passport.Passport) (map[string][6]string, error) {
	rows := map[string][]string{}
	b, e := os.ReadFile(filename)
	if e == nil {
		reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(b), "\ufeff")))
		reader.Comma = ';'
		reader.FieldsPerRecord = 13
		records, err := reader.ReadAll()
		if err != nil {
			return nil, fmt.Errorf("справочник нагрузок: %w", err)
		}
		if len(records) == 0 || records[0][0] != "Общество" {
			return nil, fmt.Errorf("неверный заголовок справочника нагрузок")
		}
		for _, row := range records[1:] {
			key := strings.Join(row[:5], "\x1f")
			if _, ok := rows[key]; ok {
				return nil, fmt.Errorf("повтор присоединения в справочнике нагрузок")
			}
			for _, value := range row[5:11] {
				if value != "" {
					v, err := strconv.ParseFloat(strings.ReplaceAll(value, ",", "."), 64)
					if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
						return nil, fmt.Errorf("неверная нагрузка %q для %s", value, row[2])
					}
				}
			}
			rows[key] = row
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	for _, p := range items {
		key := BurdenKey(p)
		if _, ok := rows[key]; ok {
			continue
		}
		values := EstimateBurden(p, passport.Options{})
		_, _, source := meterBurden(p.Meter.Type)
		row := []string{p.Company, p.Object, p.Connection, p.Meter.Serial, p.Meter.Type}
		row = append(row, values[:]...)
		row = append(row, burdenConditions, source)
		rows[key] = row
	}
	var output bytes.Buffer
	output.WriteString("\ufeff")
	writer := csv.NewWriter(&output)
	writer.Comma = ';'
	writer.Write([]string{"Общество", "Объект", "Присоединение", "Номер счётчика", "Тип счётчика", "ТТ_A_ВА", "ТТ_B_ВА", "ТТ_C_ВА", "ТН_A_ВА", "ТН_B_ВА", "ТН_C_ВА", "Условия", "Источник"})
	keys := []string{}
	for key := range rows {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := map[string][6]string{}
	for _, key := range keys {
		writer.Write(rows[key])
		var v [6]string
		copy(v[:], rows[key][5:11])
		values[key] = v
	}
	writer.Flush()
	if e := writer.Error(); e != nil {
		return nil, e
	}
	tmp, e := os.CreateTemp(filepath.Dir(filename), "burden-*.tmp")
	if e != nil {
		return nil, e
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	_, e = tmp.Write(output.Bytes())
	closeErr := tmp.Close()
	if e != nil {
		return nil, e
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if e = os.Rename(tmpName, filename); e != nil {
		return nil, e
	}
	return values, nil
}
func burdenSheet(p passport.Passport, options passport.Options) []byte {
	v := EstimateBurden(p, options)
	si, su, source := meterBurden(p.Meter.Type)
	rows := [][]string{
		{"Расчёт нагрузки вторичных цепей"},
		{"Объект", p.Object}, {"Присоединение", p.Connection}, {"Счётчик", p.Meter.Type, p.Meter.Serial},
		{"Основание", "Условная верхняя оценка; измерение нагрузки не проводилось"},
		{"Условия", burdenConditions},
		{"Длина в одну сторону, м", "20"}, {"Медь, сечение, мм²", "4"}, {"Удельное сопротивление, Ом·мм²/м", "0,0175"},
		{"Сопротивление контактов, Ом", "0,02"}, {"R петли с контактами, Ом", "0,195"}, {"Фазное напряжение, В", "57,7"},
		{"Прибор по току, ВА", decimal(si)}, {"Прибор по напряжению, ВА", decimal(su)},
		{"Фаза", "Тип ТТ", "Тип ТН", "Нагрузка ТТ, ВА", "Нагрузка ТН, ВА"},
	}
	for i, phase := range []string{"A", "B", "C"} {
		rows = append(rows, []string{phase, p.CT[i].Type, p.VT[i].Type, v[i], v[3+i]})
	}
	rows = append(rows, []string{"Формула ТТ", "Sприбора + I2² × (0,0175 × 2 × L / q + Rконт)"}, []string{"Формула ТН", "Sприбора + (Sприбора / Uф)² × Rпетли"}, []string{"Источник", source}, []string{"Справочник", BurdenCatalogName + "; шесть столбцов нагрузок можно уточнить отдельно для каждого присоединения"})
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><cols><col min="1" max="1" width="42" customWidth="1"/><col min="2" max="3" width="55" customWidth="1"/><col min="4" max="5" width="24" customWidth="1"/></cols><sheetData>`)
	for r, row := range rows {
		b.WriteString(fmt.Sprintf(`<row r="%d">`, r+1))
		for c, value := range row {
			b.WriteString(textCell(xlsx.Column(c+1)+strconv.Itoa(r+1), "8", value))
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData></worksheet>`)
	return []byte(b.String())
}
