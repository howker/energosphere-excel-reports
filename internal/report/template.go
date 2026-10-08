package report

import _ "embed"

// Template is sanitized from the supplied П-П1; contains no example readings,
// equipment, VBA, database metadata or lookup values.
//
//go:embed assets/passport.xlsx
var Template []byte
