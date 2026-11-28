package whisper

import (
	"regexp"
	"sort"
	"strings"
	"unicode"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/normalise"
)

// thousands is a number written with a thousands separator: "15,000".
var thousands = regexp.MustCompile(`\b(\d{1,3}),(\d{3})\b`)

// Written rewrites what whisper writes as text back to what was said, for
// the aircraft on frequency: an identifier ("DLH4EK", "CSA 1234",
// "OK-ABC") becomes its spoken callsign ("Lufthansa four echo kilo"),
// and a thousands separator is dropped ("15,000" is one number). Spoken
// then spells the rest.
func Written(text string, callsigns []voicegoio.Callsign) string {
	text = thousands.ReplaceAllString(text, "${1}${2}")
	cs := append([]voicegoio.Callsign(nil), callsigns...)
	// Longest first: "CSA1234" before a "CSA12" on frequency too.
	sort.SliceStable(cs, func(i, j int) bool { return len(cs[i].ICAO) > len(cs[j].ICAO) })
	for _, c := range cs {
		re := writtenID(c.ICAO)
		if re == nil {
			continue
		}
		s := c.Spoken
		if s == "" {
			s = normalise.SpokenCallsign(c.ICAO)
		}
		text = re.ReplaceAllLiteralString(text, s)
	}
	return text
}

// writtenID matches an identifier as whisper may write it: the letter
// prefix and the rest together or apart ("CSA1234", "CSA 1234"), a
// registration with or without its hyphen ("OK-ABC", "OK ABC", "OKABC").
func writtenID(icao string) *regexp.Regexp {
	icao = strings.ToUpper(strings.TrimSpace(icao))
	if icao == "" {
		return nil
	}
	var parts []string
	if a, b, ok := strings.Cut(icao, "-"); ok {
		parts = []string{a, b}
	} else {
		i := strings.IndexFunc(icao, func(r rune) bool { return r >= '0' && r <= '9' })
		if i > 0 {
			parts = []string{icao[:i], icao[i:]}
		} else {
			parts = []string{icao}
		}
	}
	for i := range parts {
		parts[i] = regexp.QuoteMeta(parts[i])
	}
	return regexp.MustCompile(`(?i)\b` + strings.Join(parts, `(?:,?\s|-)?`) + `\b`)
}

var digitSaid = [10]string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}

// Spoken rewrites a whisper transcript as spoken ATC, the parser's input:
// numbers digit by digit ("Speedbird 123" → "speedbird one two three"),
// decimals with "decimal" ("118.1"), "FL350" as "flight level three five
// zero", letters glued to digits apart ("A1" → "alpha one"), hyphens and
// punctuation as spaces ("take-off" → "take off").
func Spoken(transcript string) string {
	var out []string
	for _, w := range strings.FieldsFunc(transcript, func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == '-' || r == '/' || r == '?' || r == '!' || r == ';' || r == ':' || r == '"'
	}) {
		w = strings.ToLower(strings.TrimRight(w, "."))
		if w == "" {
			continue
		}
		if strings.HasPrefix(w, "fl") && len(w) > 2 && allDigits(w[2:]) {
			out = append(out, "flight", "level")
			out = appendDigits(out, w[2:])
			continue
		}
		if a, b, ok := strings.Cut(w, "."); ok && allDigits(a) && allDigits(b) && a != "" && b != "" {
			out = appendDigits(out, a)
			out = append(out, "decimal")
			out = appendDigits(out, b)
			continue
		}
		w = strings.ReplaceAll(w, ".", "")
		if allDigits(w) {
			out = appendDigits(out, w)
			continue
		}
		if mixed(w) {
			// "A1", "4EK", "A320": a letter and digit code, spelt.
			for _, r := range w {
				if r >= '0' && r <= '9' {
					out = append(out, digitSaid[r-'0'])
				} else if s, ok := nato[r]; ok {
					out = append(out, s)
				}
			}
			continue
		}
		w = strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\'' })
		if len(w) == 1 && len(out) > 0 && letterBefore[out[len(out)-1]] {
			if s, ok := nato[rune(w[0])]; ok {
				w = s
			}
		}
		if w != "" {
			out = append(out, w)
		}
	}
	return strings.Join(out, " ")
}

var nato = map[rune]string{'a': "alpha", 'b': "bravo", 'c': "charlie", 'd': "delta", 'e': "echo", 'f': "foxtrot",
	'g': "golf", 'h': "hotel", 'i': "india", 'j': "juliett", 'k': "kilo", 'l': "lima", 'm': "mike",
	'n': "november", 'o': "oscar", 'p': "papa", 'q': "quebec", 'r': "romeo", 's': "sierra", 't': "tango",
	'u': "uniform", 'v': "victor", 'w': "whiskey", 'x': "xray", 'y': "yankee", 'z': "zulu"}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func appendDigits(out []string, d string) []string {
	for _, r := range d {
		out = append(out, digitSaid[r-'0'])
	}
	return out
}

// letterBefore are the words after which a lone letter is a taxiway said
// as a letter ("via B, A" is "via bravo alpha"): not "a" the article.
var letterBefore = func() map[string]bool {
	m := map[string]bool{"via": true, "then": true, "and": true, "point": true, "of": true, "taxiway": true,
		"stand": true, "gate": true, "to": true, "up": true}
	for _, s := range nato {
		m[s] = true
	}
	for _, s := range digitSaid {
		m[s] = true
	}
	return m
}()

// mixed reports a short code of ASCII letters and digits, both: "a1", "4ek".
func mixed(w string) bool {
	if len(w) > 7 {
		return false
	}
	var l, d bool
	for _, r := range w {
		switch {
		case r >= '0' && r <= '9':
			d = true
		case r >= 'a' && r <= 'z':
			l = true
		default:
			return false
		}
	}
	return l && d
}
