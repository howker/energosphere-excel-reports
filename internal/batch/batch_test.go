package batch

import (
	"context"
	"fmt"
	"github.com/howker/energosphere-excel-reports/internal/passport"
	"github.com/howker/energosphere-excel-reports/internal/report"
	"github.com/howker/energosphere-excel-reports/internal/xlsx"
	"strings"
	"testing"
)

func TestCancelKeepsCompleted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	items := []passport.Passport{{Row: 10, Connection: "one"}, {Row: 13, Connection: "two"}}
	r := Run(ctx, report.Template, items, t.TempDir(), "", func(p Progress) {
		if p.Done == 1 {
			cancel()
		}
	})
	if !r.Cancelled || len(r.Files) != 1 || len(r.Errors) != 0 {
		t.Fatalf("%+v", r)
	}
}
func TestErrorDoesNotStopOtherReports(t *testing.T) {
	items := []passport.Passport{{Row: 10, Connection: string(make([]rune, 300))}, {Row: 13, Connection: "two"}}
	// Use an actual long name, not control characters that sanitize to spaces.
	for i := 0; i < 300; i++ {
		items[0].Connection += "я"
	}
	r := Run(context.Background(), report.Template, items, t.TempDir(), "", nil)
	if len(r.Errors) != 1 || len(r.Files) != 1 {
		t.Fatalf("%+v", r)
	}
}

// Проверяется нумерация выбранных паспортов независимо от исходных строк.
func TestNumberingSelectedReports(t *testing.T) {
	items := []passport.Passport{{Row: 10, Connection: "СШ1 яч.11"}, {Row: 16, Connection: "СШ2 яч.5"}}
	result := RunWithOptions(context.Background(), report.Template, items, t.TempDir(), passport.Options{StartNumber: 41}, nil)
	if len(result.Errors) != 0 || len(result.Files) != 2 {
		t.Fatalf("%+v", result)
	}
	for i, file := range result.Files {
		book, err := xlsx.Open(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, sheet := range book.Sheets {
			if sheet.Name == "Лист1" {
				got, _ := book.Raw(sheet.Cells["A7"])
				want := fmt.Sprintf("ПАСПОРТ-ПРОТОКОЛ № %d", 41+i)
				if got != want {
					t.Fatalf("%q want %q", got, want)
				}
			}
		}
	}
}
func TestFailedExportDoesNotConsumeNumber(t *testing.T) {
	items := []passport.Passport{{Row: 10, Connection: strings.Repeat("я", 300)}, {Row: 16, Connection: "ok"}}
	result := RunWithOptions(context.Background(), report.Template, items, t.TempDir(), passport.Options{StartNumber: 75}, nil)
	if len(result.Errors) != 1 || len(result.Files) != 1 {
		t.Fatalf("%+v", result)
	}
	book, err := xlsx.Open(result.Files[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, sheet := range book.Sheets {
		if sheet.Name == "Лист1" {
			got, _ := book.Raw(sheet.Cells["A7"])
			if got != "ПАСПОРТ-ПРОТОКОЛ № 75" {
				t.Fatal(got)
			}
		}
	}
}
