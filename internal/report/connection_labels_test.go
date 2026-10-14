package report

import "testing"

func TestConnectionLabels(t *testing.T) {
	for _, tc := range []struct{ source, section, cell string }{
		{"СШ1 ЗРУ 6кВ №1 Ввод 1 яч. №11", "Секция шин 1", "Ячейка № 11"},
		{"СШ 2 ЗРУ 10кВ яч.48", "Секция шин 2", "Ячейка № 48"},
		{"Секция шин II, ячейка № 5А", "Секция шин II", "Ячейка № 5А"},
		{"ЗРУ 6кВ№1 РП-17", "Секция шин", ""},
	} {
		section, cell := connectionLabels(tc.source)
		if section != tc.section || cell != tc.cell {
			t.Errorf("%s: %q, %q", tc.source, section, cell)
		}
	}
}
