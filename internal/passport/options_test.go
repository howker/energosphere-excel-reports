package passport

import "testing"

func TestCommissionAndDate(t *testing.T) {
	m, e := ParseCommission("Инженер АОСС; С.Е. Кудряшов\r\nНачальник ________ И.И. Иванов\n")
	if e != nil || len(m) != 2 || m[0].Signature() != "Инженер АОСС  ________  С.Е. Кудряшов" {
		t.Fatalf("%+v %v", m, e)
	}
	if _, e := ParseCommission("только должность"); e == nil {
		t.Fatal("ambiguous input accepted")
	}
	if e := (Options{CompilationDate: "31.02.2026"}).Validate(); e == nil {
		t.Fatal("bad date accepted")
	}
	if e := (Options{CompilationDate: "08.10.2026"}).Validate(); e != nil {
		t.Fatal(e)
	}
}
