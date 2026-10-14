package report

import (
	"regexp"
	"strings"
)

var sectionNumberRE = regexp.MustCompile(`(?i)(?:^|[\s,;])(?:СШ|секция(?:\s+шин)?|секц\.?|сек\.)\s*№?\s*([0-9]+(?:[./-][0-9]+)?|[IVX]+)(?:[^\p{L}\p{N}]|$)`)
var cellNumberRE = regexp.MustCompile(`(?i)(?:^|[\s,;])(?:ячейка|яч\.?|ячейки)\s*№?\s*([0-9]+(?:[./-][0-9]+)?[\p{L}]?)(?:[^\p{L}\p{N}]|$)`)

// Подписи основаны только на явно указанных секции и ячейке источника.
func connectionLabels(connection string) (string, string) {
	section, cell := "Секция шин", ""
	if m := sectionNumberRE.FindStringSubmatch(connection); len(m) > 1 {
		section += " " + strings.TrimSpace(m[1])
	}
	if m := cellNumberRE.FindStringSubmatch(connection); len(m) > 1 {
		cell = "Ячейка № " + strings.TrimSpace(m[1])
	}
	return section, cell
}
