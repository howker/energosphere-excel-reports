package batch

import (
	"context"
	"github.com/howker/energosphere-excel-reports/internal/passport"
	"github.com/howker/energosphere-excel-reports/internal/report"
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
