// Development utility. Never needed on the user's computer.
package main

import (
	"fmt"
	"github.com/howker/energosphere-excel-reports/internal/passport"
	"github.com/howker/energosphere-excel-reports/internal/report"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "templateprep source.xlsm destination.xlsx")
		os.Exit(2)
	}
	b, e := os.ReadFile(os.Args[1])
	if e == nil {
		b, e = report.Build(b, passport.Passport{})
	}
	if e == nil {
		e = os.WriteFile(os.Args[2], b, 0666)
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
