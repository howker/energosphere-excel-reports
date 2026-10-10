package batch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/howker/energosphere-excel-reports/internal/passport"
	"github.com/howker/energosphere-excel-reports/internal/report"
)

type Progress struct {
	Done, Total, Success int
	Current, File        string
	Err                  error
}
type Result struct {
	Files     []string
	Errors    []string
	Cancelled bool
}

func Run(ctx context.Context, template []byte, items []passport.Passport, dir, prefix string, notify func(Progress)) Result {
	return RunWithOptions(ctx, template, items, dir, passport.Options{Prefix: prefix}, notify)
}
func RunWithOptions(ctx context.Context, template []byte, items []passport.Passport, dir string, options passport.Options, notify func(Progress)) Result {
	r := Result{}
	if err := options.Validate(); err != nil {
		r.Errors = append(r.Errors, err.Error())
		return r
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		r.Errors = append(r.Errors, err.Error())
		return r
	}
	catalogItems := options.BurdenCatalogItems
	if len(catalogItems) == 0 {
		catalogItems = items
	}
	overrides, err := report.PrepareBurdenCatalog(filepath.Join(dir, report.BurdenCatalogName), catalogItems)
	if err != nil {
		r.Errors = append(r.Errors, err.Error())
		return r
	}
	options.BurdenOverrides = overrides
	for i, p := range items {
		select {
		case <-ctx.Done():
			r.Cancelled = true
			return r
		default:
		}
		if notify != nil {
			notify(Progress{Done: i, Total: len(items), Success: len(r.Files), Current: p.Connection})
		}
		file, err := report.SaveWithOptions(template, p, dir, options)
		if err != nil {
			r.Errors = append(r.Errors, fmt.Sprintf("Строка %d, %s: %v", p.Row, p.Connection, err))
		} else {
			r.Files = append(r.Files, file)
		}
		if notify != nil {
			notify(Progress{Done: i + 1, Total: len(items), Success: len(r.Files), Current: p.Connection, File: file, Err: err})
		}
	}
	return r
}
