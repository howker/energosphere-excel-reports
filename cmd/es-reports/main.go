package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/howker/energosphere-excel-reports/internal/batch"
	"github.com/howker/energosphere-excel-reports/internal/importer"
	"github.com/howker/energosphere-excel-reports/internal/passport"
	"github.com/howker/energosphere-excel-reports/internal/report"
)

func main() {
	if len(os.Args) == 1 {
		if err := runGUI("", "", ""); err != nil {
			fmt.Fprintln(os.Stderr, err)
			showError(err.Error())
		}
		return
	}
	source := flag.String("source", "", "Исходный XLSX")
	out := flag.String("out", "reports", "Папка паспортов")
	prefix := flag.String("prefix", "", "Общий верхний уровень имени")
	rows := flag.String("rows", "", "Исходные строки, например 10,16; пусто = все")
	list := flag.Bool("list", false, "Только список присоединений")
	gui := flag.Bool("gui", false, "Открыть окно; --source и --out необязательны")
	flag.Parse()
	if *gui {
		if err := runGUI(*source, *out, *prefix); err != nil {
			fmt.Fprintln(os.Stderr, err)
			showError(err.Error())
		}
		return
	}
	if *source == "" {
		flag.Usage()
		os.Exit(2)
	}
	items, err := importer.Load(*source)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *list {
		for _, p := range items {
			fmt.Printf("%d\t%s\t%s\t%s\n", p.Row, p.Object, p.Connection, p.Meter.Serial)
		}
		return
	}
	if *rows != "" {
		wanted := map[int]bool{}
		for _, s := range strings.Split(*rows, ",") {
			n, e := strconv.Atoi(strings.TrimSpace(s))
			if e != nil {
				fmt.Fprintln(os.Stderr, "Неверный --rows")
				os.Exit(2)
			}
			wanted[n] = true
		}
		var picked []passport.Passport
		for _, p := range items {
			if wanted[p.Row] {
				picked = append(picked, p)
				delete(wanted, p.Row)
			}
		}
		if len(wanted) > 0 {
			fmt.Fprintln(os.Stderr, "Указанные строки отсутствуют в источнике")
			os.Exit(2)
		}
		items = picked
	}
	r := batch.Run(context.Background(), report.Template, items, *out, *prefix, func(p batch.Progress) {
		if p.File != "" {
			fmt.Printf("%d/%d %s\n", p.Done, p.Total, p.File)
		}
	})
	for _, e := range r.Errors {
		fmt.Fprintln(os.Stderr, e)
	}
	fmt.Printf("Готово: %d; ошибок: %d\n", len(r.Files), len(r.Errors))
	if len(r.Errors) > 0 {
		os.Exit(1)
	}
}
