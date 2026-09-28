// Package data embeds the library's static lookup tables.
//
// It exists as its own package because go:embed cannot reach outside the
// directory of the package that declares it, and SPEC.md places the tables at
// the repository root rather than inside the consumer package.
package data

import _ "embed"

// TelephonyCSV maps an ICAO three letter airline designator to its radio
// telephony designator, one "CODE,Telephony" record per line. Comment lines
// start with '#'.
//
//go:embed telephony.csv
var TelephonyCSV string
