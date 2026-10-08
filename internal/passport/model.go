package passport

import "strings"

// Equipment keeps source strings: identifiers and dates must not be inferred.
type Equipment struct {
	Type, Serial, Accuracy, Year, Ratio         string
	Verification, NextVerification, Certificate string
}

type Passport struct {
	Row                                                         int
	Company, Object, Inventory, Connection, Voltage, Accounting string
	Exchange, Ownership                                         string
	Meter                                                       Equipment
	CT, VT                                                      [3]Equipment // Source positions top to bottom, interpreted as A/B/C.
	SourceCells                                                 [3][32]string
}

func (p Passport) Chain(prefix string) string {
	parts := []string{prefix, p.Company, p.Object, p.Connection}
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}
