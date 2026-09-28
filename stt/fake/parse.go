// Package fake provides the non Windows speech recognition backends: a parser
// over spoken ATC text, a stdin driven recogniser for development, and a script
// driven one for regression runs.
//
// The parser is a hand written equivalent of grammar/atc.grxml. Having the same
// phrases tagged the same way by two independent implementations is what lets
// one corpus (testdata/stt/*.jsonl) validate both the Windows SAPI backend and
// everything that runs on the development machine.
package fake

import (
	"strconv"
	"strings"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/normalise"
)

// Parser turns spoken pilot text into semantic tags. Callsign resolution is
// restricted to the aircraft on frequency, mirroring the dynamic rule the SAPI
// grammar rebuilds on every roster change.
type Parser struct {
	// spoken form (lower case) -> ICAO identifier
	roster map[string]string
}

// NewParser returns a parser with an empty roster.
func NewParser() *Parser { return &Parser{roster: map[string]string{}} }

// SetCallsigns replaces the roster. Every phraseology variant of a callsign is
// registered, so "Speedbird one two three" and "Speedbird one two tree" both
// resolve to BAW123.
func (p *Parser) SetCallsigns(cs []voicegoio.Callsign) {
	r := make(map[string]string, len(cs)*2)
	for _, c := range cs {
		spoken := c.Spoken
		if spoken == "" {
			spoken = normalise.SpokenCallsign(c.ICAO)
		}
		for _, v := range normalise.SpokenVariants(spoken) {
			r[strings.ToLower(v)] = strings.ToUpper(c.ICAO)
		}
	}
	p.roster = r
}

// digitWord maps every spoken digit, in both phraseologies, to its character.
var digitWord = map[string]byte{
	"zero": '0', "oh": '0', "one": '1', "two": '2',
	"three": '3', "tree": '3', "four": '4', "fower": '4',
	"five": '5', "fife": '5', "six": '6', "seven": '7',
	"eight": '8', "nine": '9', "niner": '9',
}

var letterWord = map[string]byte{
	"alpha": 'A', "bravo": 'B', "charlie": 'C', "delta": 'D', "echo": 'E',
	"foxtrot": 'F', "golf": 'G', "hotel": 'H', "india": 'I', "juliett": 'J',
	"juliet": 'J', "kilo": 'K', "lima": 'L', "mike": 'M', "november": 'N',
	"oscar": 'O', "papa": 'P', "quebec": 'Q', "romeo": 'R', "sierra": 'S',
	"tango": 'T', "uniform": 'U', "victor": 'V', "whiskey": 'W', "xray": 'X',
	"yankee": 'Y', "zulu": 'Z',
}

// Parse returns the recognition for one spoken transmission.
//
// Confidence is synthetic here: 0.95 when a known callsign and a known intent
// were both found, 0.6 when only one of the two was, and 0 for anything the
// grammar would have rejected. The application applies the same threshold it
// applies to the real engine, so the two backends behave alike.
func (p *Parser) Parse(text string) voicegoio.Recognition {
	toks := tokenize(text)
	rec := voicegoio.Recognition{Text: text, Tags: map[string]string{}}

	callsign, rest := p.matchCallsign(toks)
	if callsign != "" {
		rec.Tags[voicegoio.TagCallsign] = callsign
	}
	intent, value := parseBody(rest)
	if intent == "" {
		// A transmission may put the callsign last: "descending flight level
		// one zero zero, Speedbird one two three".
		if intent, value = parseBody(toks); intent == "" {
			rec.Tags[voicegoio.TagIntent] = voicegoio.IntentSayAgain
			return rec
		}
	}
	rec.Tags[voicegoio.TagIntent] = intent
	if value != "" {
		rec.Tags[voicegoio.TagValue] = value
	}
	switch {
	case callsign != "" && intent != voicegoio.IntentSayAgain:
		rec.Confidence = 0.95
	case intent == "mayday" || intent == "pan_pan":
		// An aircraft in distress may not be on the roster at all — it may have
		// just switched to this frequency precisely because it is in trouble.
		// Report it confidently and let the controller ask who is calling.
		rec.Confidence = 0.9
	default:
		rec.Confidence = 0.6
	}
	return rec
}

func tokenize(s string) []string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == ' ' || r == '-' {
			return r
		}
		return ' '
	}, s)
	return strings.Fields(s)
}

// matchCallsign finds the longest roster entry anywhere in the transmission and
// returns the words around it.
//
// Anywhere, not just at the start or the end: a distress call puts the signal
// first and the callsign after it — "mayday mayday mayday, Speedbird one two
// three, engine failure" — and that is the one transmission that must not be
// missed.
func (p *Parser) matchCallsign(toks []string) (string, []string) {
	for n := min(len(toks), 8); n >= 2; n-- {
		for start := 0; start+n <= len(toks); start++ {
			icao, ok := p.roster[strings.Join(toks[start:start+n], " ")]
			if !ok {
				continue
			}
			rest := make([]string, 0, len(toks)-n)
			rest = append(rest, toks[:start]...)
			rest = append(rest, toks[start+n:]...)
			return icao, rest
		}
	}
	return "", toks
}

// digits collects consecutive spoken digits starting at i.
func digits(toks []string, i int) (string, int) {
	var b []byte
	for ; i < len(toks); i++ {
		d, ok := digitWord[toks[i]]
		if !ok {
			break
		}
		b = append(b, d)
	}
	return string(b), i
}

// letters collects consecutive NATO letters, with optional trailing digit, as
// used by taxiway identifiers (alpha three -> A3).
func letters(toks []string, i int) (string, int) {
	var b []byte
	for ; i < len(toks); i++ {
		if l, ok := letterWord[toks[i]]; ok {
			b = append(b, l)
			continue
		}
		if d, ok := digitWord[toks[i]]; ok && len(b) > 0 {
			b = append(b, d)
			continue
		}
		break
	}
	return string(b), i
}

// level parses any of the ways an altitude is spoken:
// "flight level three five zero" -> FL350, "five thousand" -> 5000,
// "two thousand five hundred" -> 2500.
func level(toks []string, i int) (string, int) {
	if i+1 < len(toks) && toks[i] == "flight" && toks[i+1] == "level" {
		d, j := digits(toks, i+2)
		if d == "" {
			return "", i
		}
		return "FL" + d, j
	}
	d, j := digits(toks, i)
	if d == "" {
		return "", i
	}
	if j < len(toks) && toks[j] == "thousand" {
		v, _ := strconv.Atoi(d)
		total := v * 1000
		j++
		if d2, k := digits(toks, j); d2 != "" && k < len(toks) && toks[k] == "hundred" {
			h, _ := strconv.Atoi(d2)
			total += h * 100
			j = k + 1
		}
		return strconv.Itoa(total), j
	}
	if j < len(toks) && toks[j] == "hundred" {
		v, _ := strconv.Atoi(d)
		return strconv.Itoa(v * 100), j + 1
	}
	return d, j
}

// frequency parses "one two seven decimal four five" -> 127.45.
func frequency(toks []string, i int) (string, int) {
	whole, j := digits(toks, i)
	if len(whole) < 3 || j >= len(toks) {
		return "", i
	}
	if toks[j] != "decimal" && toks[j] != "point" {
		return "", i
	}
	frac, k := digits(toks, j+1)
	if frac == "" {
		return "", i
	}
	return whole + "." + frac, k
}

// phrase reports whether toks contains the given consecutive words, and where.
func phrase(toks []string, words ...string) (int, bool) {
	for i := 0; i+len(words) <= len(toks); i++ {
		ok := true
		for j, w := range words {
			if toks[i+j] != w {
				ok = false
				break
			}
		}
		if ok {
			return i + len(words), true
		}
	}
	return 0, false
}

func any(toks []string, words ...string) (int, bool) {
	for _, w := range words {
		if i, ok := phrase(toks, strings.Fields(w)...); ok {
			return i, true
		}
	}
	return 0, false
}

// parseBody is the equivalent of the "body" and "short" rules in atc.grxml.
// The order of the checks is the priority order: a transmission that contains
// both "say again" and something else is a say again.
func parseBody(toks []string) (intent, value string) {
	if len(toks) == 0 {
		return "", ""
	}
	// Distress and urgency outrank everything, including "say again": a
	// transmission that contains "mayday" is a mayday whatever else is in it,
	// and misclassifying one is the worst thing this parser can do.
	if _, ok := phrase(toks, "mayday"); ok {
		return "mayday", emergencyNature(toks)
	}
	if _, ok := phrase(toks, "pan", "pan"); ok {
		return "pan_pan", emergencyNature(toks)
	}
	if _, ok := phrase(toks, "say", "again"); ok {
		return voicegoio.IntentSayAgain, ""
	}
	for _, w := range []string{"standby", "roger", "wilco", "affirm", "negative"} {
		if _, ok := phrase(toks, w); ok {
			return w, ""
		}
	}

	// A transmission that says "request" is a request even when it also
	// contains a word the readback rules match, such as "request climb".
	requested := has(toks, "request", "requesting", "we would like")
	if requested {
		if intent, value = parseRequest(toks); intent != "" {
			return intent, value
		}
	}

	// --- readbacks -----------------------------------------------------
	if i, ok := any(toks, "climbing to", "descending to", "climbing", "descending",
		"climb to", "descend to", "maintaining", "maintain", "climb", "descend"); ok {
		if v, _ := level(toks, skipWord(toks, i, "to")); v != "" {
			return "readback_altitude", v
		}
	}
	if i, ok := any(toks, "turn left heading", "turn right heading", "fly heading", "heading"); ok {
		if v, _ := digits(toks, i); len(v) == 3 {
			return "readback_heading", v
		}
	}
	if i, ok := any(toks, "reduce speed", "increase speed", "maintain speed", "speed"); ok {
		if v, _ := digits(toks, i); v != "" {
			return "readback_speed", v
		}
	}
	if i, ok := any(toks, "contact", "monitoring", "over to", "going to"); ok {
		if v, j := frequency(toks, skipStation(toks, i)); v != "" {
			_ = j
			return "readback_frequency", v
		}
	}
	// A frequency read back on its own, with no "contact" in front of it, which
	// is how most pilots actually do it: "one one eight decimal five, Speedbird
	// one two three".
	if v, _ := frequency(toks, 0); v != "" {
		return "readback_frequency", v
	}
	if i, ok := any(toks, "squawking", "squawk"); ok {
		if v, _ := digits(toks, i); len(v) == 4 {
			return "readback_squawk", v
		}
	}
	if i, ok := any(toks, "qnh", "altimeter"); ok {
		if v, _ := digits(toks, i); len(v) == 4 {
			return "readback_qnh", v
		}
	}
	if i, ok := any(toks, "taxiing via", "taxi via", "via"); ok {
		if v := route(toks, i); v != "" {
			return "readback_taxi", v
		}
	}
	if i, ok := any(toks, "runway"); ok {
		if v := runway(toks, i); v != "" {
			return "readback_runway", v
		}
	}

	if !requested {
		if intent, value = parseRequest(toks); intent != "" {
			return intent, value
		}
	}

	// --- reports -------------------------------------------------------
	if _, ok := any(toks, "ready for departure", "ready for takeoff", "ready"); ok {
		return "report_ready", ""
	}
	if _, ok := any(toks, "established on the localizer", "localizer established", "established"); ok {
		return "report_established", ""
	}
	if _, ok := any(toks, "runway vacated", "vacated", "clear of the runway"); ok {
		return "report_vacated", ""
	}
	if _, ok := any(toks, "going around", "go around"); ok {
		return "report_going_around", ""
	}
	if _, ok := any(toks, "field in sight", "runway in sight", "traffic in sight"); ok {
		return "report_in_sight", ""
	}

	// --- check in ------------------------------------------------------
	if i, ok := phrase(toks, "flight", "level"); ok {
		// "London Control, Speedbird one two three, flight level tree six
		// zero" is a check-in at a level, and the level is what the controller
		// needs: FL360, not the digits that happen to follow the word.
		v, _ := level(toks, i-2)
		return "checkin", v
	}
	if i, ok := any(toks, "with you", "passing", "level at", "level"); ok {
		v, _ := level(toks, i)
		return "checkin", v
	}
	return "", ""
}

// parseRequest is the "request" rule of atc.grxml.
func parseRequest(toks []string) (intent, value string) {
	if _, ok := any(toks, "request", "requesting", "we would like"); ok {
		switch {
		case has(toks, "pushback", "push back"):
			return "request_pushback", ""
		case has(toks, "taxi"):
			return "request_taxi", ""
		case has(toks, "takeoff", "departure"):
			return "request_takeoff", ""
		case has(toks, "landing"):
			return "request_landing", ""
		case has(toks, "clearance", "ifr clearance", "start up"):
			return "request_clearance", ""
		}
	}
	if i, ok := any(toks, "request direct", "requesting direct", "direct to", "direct"); ok {
		if v, _ := letters(toks, i); len(v) >= 3 {
			return "request_direct", v
		}
		return "request_direct", ""
	}
	if i, ok := any(toks, "request climb", "requesting climb", "request higher", "higher"); ok {
		v, _ := level(toks, skipWord(toks, i, "to"))
		return "request_climb", v
	}
	if i, ok := any(toks, "request descent", "requesting descent", "request lower", "lower"); ok {
		v, _ := level(toks, skipWord(toks, i, "to"))
		return "request_descent", v
	}
	if i, ok := any(toks, "ils approach", "the ils", "visual approach", "rnav approach"); ok {
		return "request_approach", runway(toks, i)
	}

	return "", ""
}

// emergencyNature extracts what is wrong, when the pilot says so in one of the
// standard forms. It is best effort: the intent is what the controller acts
// on, and an unrecognised nature must never downgrade a mayday.
var natures = map[string]string{
	"engine failure": "engine_failure", "engine fire": "engine_fire",
	"fire on board": "fire", "smoke in the cockpit": "smoke", "smoke": "smoke",
	"low fuel": "low_fuel", "fuel emergency": "low_fuel", "minimum fuel": "low_fuel",
	"medical emergency": "medical", "medical": "medical",
	"hydraulic failure": "hydraulic", "electrical failure": "electrical",
	"depressurisation": "depressurisation", "depressurization": "depressurisation",
	"bird strike": "bird_strike", "engine out": "engine_failure",
	"gear problem": "gear", "unlawful interference": "hijack",
}

func emergencyNature(toks []string) string {
	best := ""
	bestLen := 0
	for phraseText, code := range natures {
		words := strings.Fields(phraseText)
		if _, ok := phrase(toks, words...); ok && len(words) > bestLen {
			best, bestLen = code, len(words)
		}
	}
	return best
}

func has(toks []string, words ...string) bool {
	_, ok := any(toks, words...)
	return ok
}

func skipWord(toks []string, i int, w string) int {
	if i < len(toks) && toks[i] == w {
		return i + 1
	}
	return i
}

// skipStation steps over "tower", "approach" and friends between "contact" and
// the frequency.
func skipStation(toks []string, i int) int {
	stations := map[string]bool{
		"tower": true, "ground": true, "approach": true, "departure": true,
		"center": true, "centre": true, "radar": true, "director": true,
		"delivery": true, "on": true,
	}
	for i < len(toks) && stations[toks[i]] {
		i++
	}
	// A named station ("praha radar") may precede the known word.
	for i < len(toks) {
		if _, isDigit := digitWord[toks[i]]; isDigit {
			break
		}
		i++
	}
	return i
}

func runway(toks []string, i int) string {
	d, j := digits(toks, i)
	if len(d) < 1 || len(d) > 2 {
		return ""
	}
	if len(d) == 1 {
		d = "0" + d
	}
	if j < len(toks) {
		switch toks[j] {
		case "left":
			return d + "L"
		case "right":
			return d + "R"
		case "center", "centre":
			return d + "C"
		}
	}
	return d
}

// route parses a taxi route: "alpha three then bravo" -> "A3 B".
func route(toks []string, i int) string {
	var parts []string
	for i < len(toks) {
		if toks[i] == "then" || toks[i] == "and" {
			i++
			continue
		}
		v, j := letters(toks, i)
		if v == "" {
			break
		}
		parts = append(parts, v)
		i = j
	}
	return strings.Join(parts, " ")
}
