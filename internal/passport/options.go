package passport

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

type Member struct{ Role, Name string }

func (m Member) Signature() string { return m.Role + "  ________  " + m.Name }

type Options struct {
	Prefix             string
	OmitCompany        bool
	CompilationDate    string
	VafVerification    string
	Commission         []Member
	BurdenOverrides    map[string][6]string
	BurdenCatalogItems []Passport
}

// Commission accepts one member per line: role; name. A pasted signature
// example (role ________ name) is accepted too.
func ParseCommission(text string) ([]Member, error) {
	var out []Member
	for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ";", 2)
		if len(parts) != 2 {
			parts = regexp.MustCompile(`_{2,}`).Split(line, 2)
		}
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("член комиссии, строка %d: введите должность; ФИО", i+1)
		}
		out = append(out, Member{Role: strings.TrimSpace(parts[0]), Name: strings.TrimSpace(parts[1])})
	}
	return out, nil
}
func (o Options) Validate() error {
	if o.CompilationDate != "" {
		if _, e := time.Parse("02.01.2006", o.CompilationDate); e != nil {
			return fmt.Errorf("дата составления: введите действительную дату ДД.ММ.ГГГГ")
		}
	}
	if o.VafVerification != "" {
		if _, e := time.Parse("02.01.2006", o.VafVerification); e != nil {
			return fmt.Errorf("дата поверки Парма ВАФ-А: введите действительную дату ДД.ММ.ГГГГ")
		}
	}
	for i, m := range o.Commission {
		if strings.TrimSpace(m.Role) == "" || strings.TrimSpace(m.Name) == "" {
			return fmt.Errorf("член комиссии %d: нужны должность и ФИО", i+1)
		}
	}
	return nil
}
