package fake

import (
	"strings"

	voicegoio "github.com/mrlm-net/voice-goio"
)

// NewControllerParser returns a parser for the controller's side of the
// radio (TWR and GND): the hand written equivalent of grammar/controller.grxml,
// as NewParser is of atc.grxml. Its intents are the traffic engine's
// clearance verbs: startup, pushback, pushstart, taxi, upto, hold, cross,
// handoff, lineup, lineupbehind, takeoff, abort, land, goaround, vacate; and
// say_again, standby.
func NewControllerParser() *Parser {
	return &Parser{roster: map[string]string{}, controller: true}
}

// UseControllerGrammar has r tag controller transmissions
// (NewControllerParser), keeping its roster.
func (r *Recognizer) UseControllerGrammar() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.parser.controller = true
}

// parseController is Parse for the controller side: the callsign anywhere,
// one instruction, its value and the further tags.
func (p *Parser) parseController(text string) voicegoio.Recognition {
	rec := voicegoio.Recognition{Text: text, Tags: map[string]string{}}
	callsign, rest := p.matchCallsign(controllerTokens(tokenize(text)))
	if callsign != "" {
		rec.Tags[voicegoio.TagCallsign] = callsign
	}
	intent, value := instruction(rest, rec.Tags)
	if intent == "" {
		rec.Tags[voicegoio.TagIntent] = voicegoio.IntentSayAgain
		if callsign == "" {
			rec.Tags[voicegoio.TagReason] = voicegoio.ReasonNoCallsign
			return rec
		}
		rec.Tags[voicegoio.TagReason] = voicegoio.ReasonOffGrammar
		rec.Confidence = 0.5
		return rec
	}
	rec.Tags[voicegoio.TagIntent] = intent
	if value != "" {
		rec.Tags[voicegoio.TagValue] = value
	}
	switch {
	case callsign != "":
		rec.Confidence = 0.95
	default:
		rec.Confidence = 0.6
	}
	return rec
}

// controllerTokens joins the words said apart or hyphenated into one:
// "take off", "take-off" are "takeoff"; "push back" "pushback"; "start up"
// "startup".
func controllerTokens(toks []string) []string {
	joined := map[string]string{"take off": "takeoff", "push back": "pushback", "start up": "startup"}
	out := make([]string, 0, len(toks))
	for i := 0; i < len(toks); i++ {
		t := strings.ReplaceAll(toks[i], "-", "")
		if i+1 < len(toks) {
			if j, ok := joined[toks[i]+" "+toks[i+1]]; ok {
				out = append(out, j)
				i++
				continue
			}
		}
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// instruction is the equivalent of the "instruction" and "short" rules in
// controller.grxml; tags gets the further tags. The order of the checks is
// the priority: a go-around or a stop outranks whatever else was said.
func instruction(toks []string, tags map[string]string) (intent, value string) {
	if len(toks) == 0 {
		return "", ""
	}
	if has(toks, "go around") {
		return "goaround", ""
	}
	if has(toks, "stop immediately", "abort takeoff", "cancel takeoff") {
		return "abort", ""
	}
	if has(toks, "say again") {
		return voicegoio.IntentSayAgain, ""
	}
	if has(toks, "standby") {
		return "standby", ""
	}
	windTag(toks, tags)
	if has(toks, "cleared for takeoff") {
		return "takeoff", runwayAfter(toks)
	}
	if has(toks, "cleared to land") {
		return "land", runwayAfter(toks)
	}
	if has(toks, "line up and wait") {
		if toks[0] == "behind" {
			return "lineupbehind", runwayAfter(toks)
		}
		return "lineup", runwayAfter(toks)
	}
	if i, ok := any(toks, "contact", "monitor"); ok {
		if v, _ := frequency(toks, skipStation(toks, i)); v != "" {
			if i < len(toks) && stationWord[toks[i]] != "" {
				tags["station"] = stationWord[toks[i]]
			}
			return "handoff", v
		}
	}
	if i, ok := phrase(toks, "cross", "runway"); ok {
		if v := runway(toks, i); v != "" {
			return "cross", v
		}
	}
	if i, ok := phrase(toks, "vacate"); ok {
		if i < len(toks) && (toks[i] == "left" || toks[i] == "right") {
			tags["side"] = toks[i]
		}
		if j, ok := phrase(toks, "via"); ok {
			v, _ := taxiway(toks, skipWord(toks, j, "taxiway"))
			return "vacate", v
		}
		return "vacate", ""
	}
	if has(toks, "pushback and startup approved", "push and start approved") {
		return "pushstart", facing(toks)
	}
	if has(toks, "pushback approved") {
		return "pushback", facing(toks)
	}
	if has(toks, "startup approved") {
		if i, ok := any(toks, "qnh", "altimeter"); ok {
			if q, _ := digits(toks, i); len(q) == 4 {
				tags["qnh"] = q
			}
		}
		return "startup", ""
	}
	if i, ok := phrase(toks, "taxi", "via"); ok {
		if j, ok := any(toks, "up to", "hold short of taxiway", "hold short of"); ok {
			if v, _ := taxiway(toks, j); v != "" {
				tags["via"] = taxiRoute(toks, i)
				return "upto", v
			}
		}
	}
	if i, ok := phrase(toks, "taxi", "to"); ok {
		switch {
		case i+1 < len(toks) && toks[i] == "holding" && toks[i+1] == "point":
			if v, _ := taxiway(toks, i+2); v != "" {
				tags["holding_point"] = v
			}
		case i < len(toks) && (toks[i] == "stand" || toks[i] == "gate"):
			if v, _ := stand(toks, i+1); v != "" {
				tags["stand"] = v
			}
		}
		if j, ok := phrase(toks, "via"); ok {
			if v := taxiRoute(toks, j); v != "" {
				tags["via"] = v
			}
		}
		return "taxi", runwayAfter(toks)
	}
	if i, ok := phrase(toks, "hold", "short", "of", "runway"); ok {
		if v := runway(toks, i); v != "" {
			return "hold", v
		}
	}
	if i, ok := phrase(toks, "hold", "short", "of"); ok {
		if v, _ := taxiway(toks, skipWord(toks, i, "taxiway")); v != "" {
			return "hold", v
		}
	}
	if has(toks, "hold position") {
		return "hold", ""
	}
	return "", ""
}

var stationWord = map[string]string{
	"tower": "tower", "ground": "ground", "delivery": "delivery", "approach": "approach",
	"departure": "departure", "radar": "radar", "director": "director", "center": "center", "centre": "center",
}

// runwayAfter is the runway said after the first "runway", "" none.
func runwayAfter(toks []string) string {
	if i, ok := phrase(toks, "runway"); ok {
		return runway(toks, i)
	}
	return ""
}

// facing is the compass point after "facing", "" none.
func facing(toks []string) string {
	if i, ok := phrase(toks, "facing"); ok && i < len(toks) {
		switch toks[i] {
		case "north", "east", "south", "west":
			return toks[i]
		}
	}
	return ""
}

// windTag tags the wind: "wind two four zero degrees eight knots" is
// "240/8", "wind calm" "calm".
func windTag(toks []string, tags map[string]string) {
	i, ok := phrase(toks, "wind")
	if !ok {
		return
	}
	if i < len(toks) && toks[i] == "calm" {
		tags["wind"] = "calm"
		return
	}
	dir, j := digits(toks, i)
	speed := ""
	switch {
	case len(dir) == 3:
		speed, _ = digits(toks, skipWord(toks, j, "degrees"))
	case len(dir) == 4 || len(dir) == 5:
		dir, speed = dir[:3], dir[3:]
	}
	if speed != "" && len(speed) <= 2 {
		tags["wind"] = dir + "/" + speed
	}
}

// taxiway is a taxiway said at i: one letter and up to two digits ("alpha
// one" A1), as the grammar's taxiway rule; the index after it.
func taxiway(toks []string, i int) (string, int) { return ident(toks, i, 2) }

// stand is a stand or gate: one letter and up to three digits ("bravo one
// four" B14).
func stand(toks []string, i int) (string, int) { return ident(toks, i, 3) }

func ident(toks []string, i, maxDigits int) (string, int) {
	if i >= len(toks) {
		return "", i
	}
	l, ok := letterWord[toks[i]]
	if !ok {
		return "", i
	}
	b := []byte{l}
	i++
	for n := 0; n < maxDigits && i < len(toks); n++ {
		d, ok := digitWord[toks[i]]
		if !ok {
			break
		}
		b = append(b, d)
		i++
	}
	return string(b), i
}

// taxiRoute is a taxi route said at i, one taxiway after another, "then"
// or "and" between them allowed: "bravo alpha one" is "B A1".
func taxiRoute(toks []string, i int) string {
	var parts []string
	for i < len(toks) {
		if toks[i] == "then" || toks[i] == "and" {
			i++
			continue
		}
		v, j := taxiway(toks, i)
		if v == "" {
			break
		}
		parts = append(parts, v)
		i = j
	}
	return strings.Join(parts, " ")
}
