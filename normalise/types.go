package normalise

import (
	"regexp"
	"strings"
)

// manufacturers name an aircraft type when they come before its model:
// "Airbus A321", "Boeing 737", "Embraer 190", "ATR 72".
var manufacturers = map[string]bool{
	"AIRBUS": true, "BOEING": true, "EMBRAER": true, "ATR": true, "BOMBARDIER": true,
	"CRJ": true, "DASH": true, "SAAB": true, "FOKKER": true, "CESSNA": true,
}

// typeModel is a model after a manufacturer: an optional letter, two or
// three digits, an optional suffix ("A321", "A320NEO", "737", "190", "72",
// "777F").
var typeModel = regexp.MustCompile(`^([A-Z]?)([0-9]{2,3})([A-Z]{0,3})$`)

// typeWords reads a model the way crews say it: the letter as a letter,
// the number as the type is called ("three twenty-one", "seven thirty-seven",
// "one ninety", "seventy-two"), the suffix as a word ("neo", "max"). False
// when up is not a model.
func typeWords(up string) ([]string, bool) {
	m := typeModel.FindStringSubmatch(up)
	if m == nil {
		return nil, false
	}
	var out []string
	if m[1] != "" {
		out = append(out, m[1])
	}
	num := m[2]
	if len(num) == 3 {
		out = append(out, ones[num[0]-'0'])
		num = num[1:]
		switch {
		case num == "00":
			out = append(out, "hundred")
			num = ""
		case num[0] == '0':
			out = append(out, "oh", ones[num[1]-'0']) // 707: seven oh seven
			num = ""
		}
	}
	if num != "" {
		out = append(out, twoDigits(num))
	}
	if m[3] != "" {
		out = append(out, strings.ToLower(m[3]))
	}
	return out, true
}

var ones = []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}

var teens = []string{"ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"}

var tens = []string{"", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"}

// twoDigits is "21" as "twenty-one", "10" as "ten", "07" as "seven".
func twoDigits(s string) string {
	t, o := s[0]-'0', s[1]-'0'
	switch {
	case t == 0:
		return ones[o]
	case t == 1:
		return teens[o]
	case o == 0:
		return tens[t]
	}
	return tens[t] + "-" + ones[o]
}
