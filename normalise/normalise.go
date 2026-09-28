// Package normalise converts ATC message text written in the application's
// normal tokens ("BAW123 climb FL350, QNH 1013") into the spoken form a text to
// speech engine can read out ("Speedbird one two tree, climb flight level tree
// fife zero, Q N H one zero one tree").
//
// It is pure Go with no state beyond the embedded telephony table, and it is
// the first thing every transmission passes through. The rule the rest of the
// library relies on is: nothing downstream of this package ever sees a digit,
// an abbreviation or a unit symbol.
package normalise

import (
	"strconv"
	"strings"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/data"
)

// Normaliser holds the telephony table. The zero value is not usable; call New.
type Normaliser struct {
	telephony map[string]string
}

// New returns a Normaliser backed by the embedded telephony table.
func New() *Normaliser {
	return &Normaliser{telephony: parseTelephony(data.TelephonyCSV)}
}

var std = New()

// Default returns the shared Normaliser. It is read only after construction and
// safe for concurrent use.
func Default() *Normaliser { return std }

// Spoken converts one transmission body into spoken form.
func Spoken(text string, ph voicegoio.Phraseology) string { return std.Spoken(text, ph) }

// SpokenCallsign converts an ICAO flight identifier or registration into its
// spoken form using FAA digits, which is the canonical form stored in
// voicegoio.Callsign.Spoken and fed to the recogniser.
func SpokenCallsign(icao string) string { return std.SpokenCallsign(icao) }

func parseTelephony(csv string) map[string]string {
	m := make(map[string]string, 128)
	for _, line := range strings.Split(csv, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		code, name, ok := strings.Cut(line, ",")
		if !ok {
			continue
		}
		m[strings.ToUpper(strings.TrimSpace(code))] = strings.TrimSpace(name)
	}
	return m
}

// Telephony reports the radio telephony designator for an ICAO airline code.
func (n *Normaliser) Telephony(code string) (string, bool) {
	v, ok := n.telephony[strings.ToUpper(code)]
	return v, ok
}

// emergencyWords are the distress and urgency signals. They matter here for
// two reasons: "PAN" is three upper case letters and would otherwise be spelled
// "papa alpha november", and the repeats that make the signal a signal have to
// be separated by pauses rather than run together.
//
// The library pronounces what the application writes; it does not rewrite the
// phraseology. An application must send the real thing —
// "MAYDAY MAYDAY MAYDAY" or "PAN PAN, PAN PAN, PAN PAN".
//
// The value is how many words make up one signal: "mayday" is one, "pan pan"
// is two. That is what decides where the pauses go, because a pause inside
// "pan pan" would break the signal rather than separate the repeats.
var emergencyWords = map[string]int{
	"MAYDAY": 1, "PAN": 2, "SECURITE": 1,
}

// weatherWords are the METAR and ATIS abbreviations an ATIS broadcast is made
// of. Without them an ATIS reads out as phonetic letters.
var weatherWords = map[string]string{
	"RA": "rain", "SN": "snow", "DZ": "drizzle", "GR": "hail",
	"TS": "thunderstorm", "TSRA": "thunderstorm with rain",
	"SH": "showers", "SHRA": "showers of rain",
	"BR": "mist", "FG": "fog", "HZ": "haze", "FU": "smoke",
	"VRB": "variable", "NOSIG": "no significant change",
	"NSC": "no significant cloud", "SKC": "sky clear", "CLR": "clear",
	"TEMPO": "tempo", "BECMG": "becoming", "PROB30": "probability tree zero",
	"WS": "wind shear", "RVR": "R V R",
}

// cloudCover maps a METAR cloud amount to its spoken form.
var cloudCover = map[string]string{
	"FEW": "few", "SCT": "scattered", "BKN": "broken", "OVC": "overcast",
}

// nato maps a letter to its ICAO spelling alphabet word.
var nato = map[byte]string{
	'A': "alpha", 'B': "bravo", 'C': "charlie", 'D': "delta", 'E': "echo",
	'F': "foxtrot", 'G': "golf", 'H': "hotel", 'I': "india", 'J': "juliett",
	'K': "kilo", 'L': "lima", 'M': "mike", 'N': "november", 'O': "oscar",
	'P': "papa", 'Q': "quebec", 'R': "romeo", 'S': "sierra", 'T': "tango",
	'U': "uniform", 'V': "victor", 'W': "whiskey", 'X': "xray", 'Y': "yankee",
	'Z': "zulu",
}

// digitWords holds the two digit vocabularies. ICAO alters 3, 5 and 9; both
// conventions keep "niner" because "nine" is too close to the German "nein".
var digitWords = map[voicegoio.Phraseology][10]string{
	voicegoio.ICAO: {"zero", "one", "two", "tree", "four", "fife", "six", "seven", "eight", "niner"},
	voicegoio.FAA:  {"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "niner"},
}

// acronyms are spoken letter by letter or as a fixed word rather than being
// treated as identifiers to spell phonetically.
var acronyms = map[string]string{
	"ILS": "I L S", "VOR": "V O R", "NDB": "N D B", "DME": "D M E",
	"RVR": "R V R", "IFR": "I F R", "VFR": "V F R", "ETA": "E T A",
	"TCAS": "T CAS", "ATC": "A T C", "QFE": "Q F E", "QNE": "Q N E",
	"ATIS": "atis", "SID": "sid", "STAR": "star", "CAVOK": "cav oh kay",
	"QNH":  "Q N H",
	"PAPI": "papi", "FBO": "F B O", "GPU": "G P U",
}

// plainWords are short tokens that are ordinary English even when the
// application writes them in upper case. Without this set "TURN LEFT HDG 310"
// would spell "tango uniform romeo november lima echo foxtrot tango".
//
// It only matters for all upper case input: a token that was not written in
// upper case is never treated as an identifier in the first place.
var plainWords = map[string]bool{
	"TO": true, "AT": true, "AND": true, "FOR": true, "VIA": true, "ON": true,
	"OF": true, "IN": true, "UP": true, "BY": true, "NO": true, "IS": true,
	"OR": true, "AS": true, "IF": true, "WE": true, "YOU": true, "ARE": true,
	"NOT": true, "OUT": true, "OFF": true, "THE": true, "A": true, "AN": true,
	"TURN": true, "LEFT": true, "HOLD": true, "TAXI": true, "LINE": true,
	"WAIT": true, "WIND": true, "GUST": true, "STOP": true, "GO": true,
	"LAND": true, "WITH": true, "THEN": true, "NEXT": true, "SLOW": true,
	"FAST": true, "GATE": true, "PUSH": true, "EXIT": true, "BACK": true,
	"OVER": true, "NOW": true, "SAY": true, "DUE": true, "ILL": true,
}

func (n *Normaliser) digit(b byte, ph voicegoio.Phraseology) string {
	w, ok := digitWords[ph]
	if !ok {
		w = digitWords[voicegoio.ICAO]
	}
	return w[b-'0']
}

// digits speaks a run of characters one symbol at a time: digits from the
// phraseology table, letters from the NATO alphabet.
func (n *Normaliser) digits(s string, ph voicegoio.Phraseology) []string {
	out := make([]string, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			out = append(out, n.digit(c, ph))
		case c >= 'A' && c <= 'Z':
			out = append(out, nato[c])
		case c >= 'a' && c <= 'z':
			out = append(out, nato[c-'a'+'A'])
		}
	}
	return out
}

// grouped speaks a round number the way a controller says an altitude:
// 5000 -> "fife thousand", 12000 -> "one two thousand", 2500 -> "two thousand
// fife hundred".
func (n *Normaliser) grouped(v int, ph voicegoio.Phraseology) []string {
	var out []string
	if v >= 1000 {
		th := v / 1000
		if th < 10 {
			out = append(out, n.digit(byte('0'+th), ph))
		} else {
			out = append(out, n.digits(strconv.Itoa(th), ph)...)
		}
		out = append(out, "thousand")
		v %= 1000
	}
	if v >= 100 {
		out = append(out, n.digit(byte('0'+v/100), ph), "hundred")
		v %= 100
	}
	if v > 0 {
		out = append(out, n.digits(strconv.Itoa(v), ph)...)
	}
	if len(out) == 0 {
		out = append(out, n.digit('0', ph))
	}
	return out
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Spoken converts text to spoken form under the given phraseology.
func (n *Normaliser) Spoken(text string, ph voicegoio.Phraseology) string {
	if ph == "" {
		ph = voicegoio.ICAO
	}
	toks := strings.Fields(text)
	out := make([]string, 0, len(toks)*3)

	// prevValue records whether the previous group was an information unit (a
	// callsign, a level, a runway, a route). A pause belongs between one of
	// those and the instruction that follows it.
	prevValue, prevCallsign, prevKeyword := false, false, false
	emergWord, emergRun := "", 0
	prevEmerg := false

	for i := 0; i < len(toks); i++ {
		raw := toks[i]
		word, tail := splitPunct(raw)
		if word == "" {
			continue
		}
		up := strings.ToUpper(word)

		// next returns the following token stripped of punctuation, and a
		// closure to consume it once a keyword has claimed it.
		next := ""
		if i+1 < len(toks) {
			next, _ = splitPunct(toks[i+1])
		}
		nextUp := strings.ToUpper(next)
		consume := func() (string, string) {
			i++
			_, t := splitPunct(toks[i])
			return nextUp, t
		}

		var words []string
		isValue := true     // most branches below emit an information unit
		isKeyword := false  // a group introduced by a keyword: RWY 27L, FL350
		isCallsign := false // only the callsign branch sets this
		switch {
		// ---- flight level -------------------------------------------------
		case up == "FL" && isDigits(nextUp):
			isKeyword = true
			v, t := consume()
			tail = t
			words = append([]string{"flight", "level"}, n.digits(v, ph)...)
		case strings.HasPrefix(up, "FL") && isDigits(up[2:]) && len(up) > 2:
			isKeyword = true
			words = append([]string{"flight", "level"}, n.digits(up[2:], ph)...)

		// ---- heading ------------------------------------------------------
		case (up == "HDG" || up == "HEADING") && isDigits(nextUp):
			isKeyword = true
			v, t := consume()
			tail = t
			words = append([]string{"heading"}, n.digits(pad(v, 3), ph)...)

		// ---- runway -------------------------------------------------------
		case (up == "RWY" || up == "RUNWAY") && isRunway(nextUp):
			isKeyword = true
			v, t := consume()
			tail = t
			words = append([]string{"runway"}, n.runway(v, ph)...)

		// ---- taxiway ------------------------------------------------------
		case (up == "TWY" || up == "TAXIWAY") && next != "":
			isKeyword = true
			v, t := consume()
			words = append([]string{"taxiway"}, n.digits(v, ph)...)
			// A route is several designators: "TWY A3 and B" is spoken
			// "taxiway alpha tree, bravo". The comma is the point: without it
			// the whole route arrives as one unbroken run of phonetics and is
			// unreadable back.
			for {
				j := i + 1
				if j < len(toks) {
					if w, _ := splitPunct(toks[j]); strings.EqualFold(w, "and") || strings.EqualFold(w, "then") {
						j++
					}
				}
				if j >= len(toks) {
					break
				}
				w, wt := splitPunct(toks[j])
				if !isTaxiwayID(w) {
					break
				}
				if t == "" {
					t = "," // the application wrote no punctuation, so we supply the pause
				}
				words[len(words)-1] += t
				words = append(words, n.digits(w, ph)...)
				i, t = j, wt
			}
			tail = t

		// ---- squawk -------------------------------------------------------
		case (up == "SQK" || up == "SQUAWK" || up == "SSR") && isDigits(nextUp):
			isKeyword = true
			v, t := consume()
			tail = t
			words = append([]string{"squawk"}, n.digits(v, ph)...)

		// ---- pressure -----------------------------------------------------
		case (up == "QNH" || up == "ALTIMETER") && isDigits(nextUp):
			isKeyword = true
			v, t := consume()
			tail = t
			words = n.pressure(v, ph)

		// ---- units --------------------------------------------------------
		case isDigits(up) && isUnit(nextUp, "FT", "FEET"):
			isKeyword = true
			// SPEC.md 4.1: "5000 ft" is spoken "fife thousand"; the unit is
			// dropped because controllers do not say it with an altitude.
			_, t := consume()
			tail = t
			v, _ := strconv.Atoi(up)
			words = n.grouped(v, ph)
		case isDigits(up) && isUnit(nextUp, "KT", "KTS", "KNOTS"):
			isKeyword = true
			_, t := consume()
			tail = t
			words = append(n.digits(up, ph), "knots")

		// ---- frequency ----------------------------------------------------
		case isFrequency(up):
			isKeyword = true
			whole, frac, _ := strings.Cut(up, ".")
			sep := "decimal"
			if ph == voicegoio.FAA {
				sep = "point"
			}
			words = append(n.digits(whole, ph), sep)
			words = append(words, n.digits(frac, ph)...)

		// ---- bare numbers -------------------------------------------------
		case isDigits(up):
			v, _ := strconv.Atoi(up)
			// Round numbers of a thousand or more read as altitudes; anything
			// else (squawks, speeds, headings without a keyword) reads digit by
			// digit, which is always safe.
			if v >= 1000 && v%100 == 0 {
				words = n.grouped(v, ph)
			} else {
				words = n.digits(up, ph)
			}

		// ---- emergency ----------------------------------------------------
		case emergencyWords[up] > 0:
			isValue = false
			words = []string{strings.ToLower(word)}
			if up == emergWord {
				emergRun++
			} else {
				emergWord, emergRun = up, 1
			}
			// Separate the repeats of the signal, but never split a signal in
			// half: a comma goes before the start of each repeat after the
			// first, which for "pan pan" is every second word.
			unit := emergencyWords[up]
			if emergRun > 1 && (emergRun-1)%unit == 0 &&
				len(out) > 0 && !endsWithPunct(out[len(out)-1]) {
				out[len(out)-1] += ","
			}

		// ---- weather ------------------------------------------------------
		case weatherWords[up] != "":
			isValue = false
			words = strings.Fields(weatherWords[up])
		case isCloudGroup(up):
			isKeyword = true
			// "BKN020" is "broken two thousand": the three digits are hundreds
			// of feet, which is why they are not read one at a time.
			height, _ := strconv.Atoi(up[3:])
			words = append([]string{cloudCover[up[:3]]}, n.grouped(height*100, ph)...)
		case len(up) > 2 && isDigits(up[:len(up)-2]) && strings.HasSuffix(up, "KM"):
			isKeyword = true
			words = append(n.digits(up[:len(up)-2], ph), "kilometres")

		// ---- identifiers --------------------------------------------------
		case acronyms[up] != "":
			isKeyword = true
			words = strings.Fields(acronyms[up])
		case n.isCallsign(up):
			isKeyword, isCallsign = true, true
			words = n.callsignWords(up, ph)
		// Only a token the application wrote in upper case is an identifier.
		// "taxi" is a word; "TWY", "LKPR" and "A3" are things to spell.
		case word == up && isIdentifier(up) && !plainWords[up]:
			isKeyword = true
			words = n.digits(up, ph)

		default:
			// A word the application wrote in upper case is lowered, so the
			// engine reads it as a word rather than as emphasis or an
			// abbreviation. Anything else keeps the case it arrived in, which
			// is what preserves proper nouns: "London Control", not "london
			// control".
			isValue = false
			if word == up {
				words = []string{strings.ToLower(word)}
			} else {
				words = []string{word}
			}
		}

		if emergencyWords[up] == 0 {
			emergWord, emergRun = "", 0
		}

		if len(words) > 0 {
			// Insert the pause, unless the application already punctuated here.
			if len(out) > 0 && !endsWithPunct(out[len(out)-1]) {
				lower := strings.ToLower(word)
				switch {
				// The end of a distress signal is a hard break: what follows it
				// is who is calling, and it must not run on from the signal.
				case prevEmerg && emergencyWords[up] == 0:
					out[len(out)-1] += ","
				case prevCallsign || (prevValue && instructionWords[lower]):
					out[len(out)-1] += ","
				// Two keyword groups in a row are two pieces of information:
				// "holding point alpha one, runway two seven left". Only
				// keyword groups, never bare digits, or "tree fife niner zero"
				// would come apart into four separate utterances.
				case prevKeyword && isKeyword:
					out[len(out)-1] += ","
				}
			}
			// A callsign anywhere earns the pause after it, not only a leading
			// one: a distress call puts the signal first and the callsign
			// second. This is a flag rather than a re-test of the token
			// because a cloud group such as FEW030 has exactly the shape of a
			// flight number and would otherwise be treated as one.
			prevCallsign = isCallsign
			prevKeyword = isKeyword
			prevEmerg = emergencyWords[up] > 0
			prevValue = isValue
			words[len(words)-1] += tail
			out = append(out, words...)
		}
	}
	return strings.Join(out, " ")
}

// splitPunct peels sentence punctuation off the end of a token so it can be
// re-attached to the last spoken word: piper uses commas and periods for
// prosody, and dropping them makes long clearances run together.
func splitPunct(tok string) (word, tail string) {
	end := len(tok)
	for end > 0 && strings.ContainsRune(",.;:!?", rune(tok[end-1])) {
		end--
	}
	return tok[:end], tok[end:]
}

func pad(s string, n int) string {
	for len(s) < n {
		s = "0" + s
	}
	return s
}

// instructionWords start a new instruction. A controller pauses before them
// when the previous words completed an information unit, which is what makes a
// long clearance followable rather than one unbroken breath.
var instructionWords = map[string]bool{
	"climb": true, "descend": true, "maintain": true, "turn": true, "fly": true,
	"cleared": true, "clear": true, "hold": true, "taxi": true, "line": true,
	"contact": true, "monitor": true, "squawk": true, "report": true,
	"expect": true, "continue": true, "join": true, "cross": true,
	"follow": true, "behind": true, "caution": true, "resume": true, "via": true,
	"descend/maintain": true,
}

func endsWithPunct(s string) bool {
	return s != "" && strings.ContainsRune(",.;:!?", rune(s[len(s)-1]))
}

// isCloudGroup matches a METAR cloud group such as BKN020 or FEW030.
func isCloudGroup(s string) bool {
	return len(s) == 6 && cloudCover[s[:3]] != "" && isDigits(s[3:])
}

// isTaxiwayID matches a taxiway designator as the application writes it: a
// short upper case identifier such as A, B2 or L.
func isTaxiwayID(s string) bool {
	if s == "" || len(s) > 3 || s != strings.ToUpper(s) {
		return false
	}
	if s[0] < 'A' || s[0] > 'Z' {
		return false
	}
	return isAlnum(s)
}

func isUnit(tok string, units ...string) bool {
	tok = strings.TrimSuffix(tok, ".")
	for _, u := range units {
		if tok == u {
			return true
		}
	}
	return false
}

// isFrequency matches a VHF frequency such as 127.45 or 118.005.
func isFrequency(s string) bool {
	whole, frac, ok := strings.Cut(s, ".")
	return ok && isDigits(whole) && isDigits(frac) && len(whole) == 3
}

// isRunway matches 27, 27L, 09R, 36C.
func isRunway(s string) bool {
	if len(s) < 1 || len(s) > 3 {
		return false
	}
	d := s
	if c := s[len(s)-1]; c < '0' || c > '9' {
		if c != 'L' && c != 'R' && c != 'C' {
			return false
		}
		d = s[:len(s)-1]
	}
	return isDigits(d) && len(d) <= 2
}

func (n *Normaliser) runway(s string, ph voicegoio.Phraseology) []string {
	side := ""
	switch s[len(s)-1] {
	case 'L':
		side, s = "left", s[:len(s)-1]
	case 'R':
		side, s = "right", s[:len(s)-1]
	case 'C':
		side, s = "center", s[:len(s)-1]
	}
	out := n.digits(pad(s, 2), ph)
	if side != "" {
		out = append(out, side)
	}
	return out
}

// pressure speaks a QNH. ICAO reads the hectopascal value digit by digit; FAA
// converts to inches of mercury and reads that.
func (n *Normaliser) pressure(v string, ph voicegoio.Phraseology) []string {
	v = pad(v, 4)
	hpa, _ := strconv.Atoi(v)
	if ph != voicegoio.FAA {
		// ICAO reads the value as written, whichever unit it is in.
		return append([]string{"Q", "N", "H"}, n.digits(v, ph)...)
	}
	if !isHectopascals(hpa) { // already hundredths of an inch, e.g. 2992
		return append([]string{"altimeter"}, n.digits(v, ph)...)
	}
	return append([]string{"altimeter"}, n.digits(pad(strconv.Itoa(hpaToInHg(hpa)), 4), ph)...)
}

// isHectopascals distinguishes the two units a pressure may arrive in. Sea
// level pressure spans roughly 870-1085 hPa, which is 2570-3200 hundredths of
// an inch, so the two ranges never overlap.
func isHectopascals(v int) bool { return v >= 800 && v < 1200 }

// hpaToInHg converts hectopascals to hundredths of an inch of mercury.
//
// 1013 is special cased: it is the shorthand every controller and chart uses
// for the ISA datum of 1013.25 hPa, which is 29.92 inHg. Converting the bare
// integer would yield 29.91 and read wrong to any pilot.
func hpaToInHg(hpa int) int {
	f := float64(hpa)
	if hpa == 1013 {
		f = 1013.25
	}
	return int(f*100/33.8638866667 + 0.5)
}

// isIdentifier reports whether an all upper case token is a code to be spelled
// out (LKPR, A3, B) rather than an English word.
func isIdentifier(s string) bool {
	if s == "" || len(s) > 5 {
		return false
	}
	hasDigit := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			hasDigit = true
		case c >= 'A' && c <= 'Z':
		default:
			return false
		}
	}
	return hasDigit || len(s) <= 4
}

// isCallsign matches an airline flight identifier (BAW123, DLH4EK) or an
// aircraft registration (OK-ABC, N123AB, D-EFGH).
func (n *Normaliser) isCallsign(s string) bool {
	if strings.Contains(s, "-") {
		a, b, _ := strings.Cut(s, "-")
		return isAlnum(a) && isAlnum(b) && a != "" && b != ""
	}
	if len(s) >= 4 {
		if _, ok := n.telephony[s[:3]]; ok && isAlnum(s[3:]) && hasDigit(s[3:]) {
			return true
		}
	}
	// Three letters followed by digits is a flight number even when the
	// operator is not in the table; it is spelled instead of pronounced.
	if len(s) >= 4 && isAlpha(s[:3]) && hasDigit(s[3:]) && isAlnum(s[3:]) {
		return true
	}
	return false
}

func (n *Normaliser) callsignWords(s string, ph voicegoio.Phraseology) []string {
	if !strings.Contains(s, "-") && len(s) >= 4 {
		if tel, ok := n.telephony[s[:3]]; ok {
			return append(strings.Fields(tel), n.digits(s[3:], ph)...)
		}
	}
	return n.digits(strings.ReplaceAll(s, "-", ""), ph)
}

func isAlpha(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 'A' || s[i] > 'Z' {
			return false
		}
	}
	return s != ""
}

func isAlnum(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return s != ""
}

func hasDigit(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			return true
		}
	}
	return false
}

// SpokenCallsign returns the canonical spoken form of a callsign, using FAA
// digits. This is what goes into voicegoio.Callsign.Spoken and, via
// SpokenVariants, into the recogniser's dynamic rule.
func (n *Normaliser) SpokenCallsign(icao string) string {
	up := strings.ToUpper(strings.TrimSpace(icao))
	if up == "" {
		return ""
	}
	if n.isCallsign(up) {
		return strings.Join(n.callsignWords(up, voicegoio.FAA), " ")
	}
	return strings.Join(n.digits(strings.ReplaceAll(up, "-", ""), voicegoio.FAA), " ")
}

// variants lists the interchangeable spoken forms of the digits that differ
// between phraseologies. A pilot may read back either, so the recogniser has to
// accept both for every callsign.
var variants = map[string][]string{
	"three": {"three", "tree"},
	"five":  {"five", "fife"},
	"niner": {"niner", "nine"},
}

// SpokenVariants expands a spoken callsign into every phraseology variant the
// recogniser should accept. "Speedbird one two three" yields both "... three"
// and "... tree". The expansion is capped so a pathological callsign cannot
// explode the grammar.
func SpokenVariants(spoken string) []string {
	words := strings.Fields(spoken)
	out := []string{""}
	for _, w := range words {
		alts, ok := variants[strings.ToLower(w)]
		if !ok {
			alts = []string{w}
		}
		if len(out)*len(alts) > 64 {
			alts = alts[:1]
		}
		grown := make([]string, 0, len(out)*len(alts))
		for _, prefix := range out {
			for _, a := range alts {
				if prefix == "" {
					grown = append(grown, a)
				} else {
					grown = append(grown, prefix+" "+a)
				}
			}
		}
		out = grown
	}
	return out
}

// interface check: normalise satisfies the API declared in the root package.
var _ voicegoio.Normaliser = (*Normaliser)(nil)
