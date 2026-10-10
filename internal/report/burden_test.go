package report

import (
	"bytes"
	"github.com/howker/energosphere-excel-reports/internal/passport"
	"os"
	"path/filepath"
	"testing"
)

func TestBurdenEstimateAndCatalogOverride(t *testing.T) {
	p := example()
	p.Meter.Type = "Меркурий 234 ARTM2-00 PB.R"
	p.CT[0].Ratio = "3000/5"
	p.CT[2].Ratio = "100/1"
	values := EstimateBurden(p, passport.Options{})
	if values[0] != "4,98" || values[1] != "" || values[2] != "0,30" || values[3] != "2,00" {
		t.Fatalf("loads=%v", values)
	}
	file := filepath.Join(t.TempDir(), BurdenCatalogName)
	loaded, e := PrepareBurdenCatalog(file, []passport.Passport{p})
	if e != nil {
		t.Fatal(e)
	}
	if loaded[BurdenKey(p)] != values {
		t.Fatal("catalog lost phase values")
	}
	b, _ := os.ReadFile(file)
	b = bytes.Replace(b, []byte("4,98"), []byte("3,45"), 1)
	os.WriteFile(file, b, 0600)
	loaded, e = PrepareBurdenCatalog(file, []passport.Passport{p})
	if e != nil {
		t.Fatal(e)
	}
	got := EstimateBurden(p, passport.Options{BurdenOverrides: loaded})
	if got[0] != "3,45" {
		t.Fatal("edited catalog value not used")
	}
	p.Meter.Serial = "replacement"
	if EstimateBurden(p, passport.Options{BurdenOverrides: loaded})[0] != "4,98" {
		t.Fatal("old meter value reused")
	}
	b, _ = os.ReadFile(file)
	b = bytes.Replace(b, []byte("3,45"), []byte("NaN"), 1)
	os.WriteFile(file, b, 0600)
	if _, e = PrepareBurdenCatalog(file, []passport.Passport{p}); e == nil {
		t.Fatal("invalid burden accepted")
	}
}
