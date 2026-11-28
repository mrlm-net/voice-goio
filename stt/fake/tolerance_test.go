package fake

import (
	"testing"

	voicegoio "github.com/mrlm-net/voice-goio"
)

func TestTolerance(t *testing.T) {
	p := NewParser()
	p.SetCallsigns([]voicegoio.Callsign{{ICAO: "DLH4EK"}, {ICAO: "CSA1234"}, {ICAO: "BAW123"}, {ICAO: "OK-ABC"}, {ICAO: "RYR89AB"}})
	for _, c := range []struct {
		say, intent, callsign, value string
		conf                         float32
	}{
		{"lafansa four echo kilo turn left heading two four zero", "readback_heading", "DLH4EK", "240", 0.85},
		{"CSA Lines one two tree four iden", "ident", "CSA1234", "", 0.95},
		{"Speedbird one two tree stand by", "standby", "BAW123", "", 0.95},
		{"Speedbird one two tree wilko", "wilco", "BAW123", "", 0.95},
		{"Oscar kilo alpha bravo charlie request push", "request_pushback", "OK-ABC", "", 0.95},
		{"made a made a made a Speedbird one two tree engine failure", "mayday", "BAW123", "engine_failure", 0.95},
		{"Speedbird one two tree squawk for tree two one", "readback_squawk", "BAW123", "4321", 0.95},
		{"Speedbird one two tree request descend", "request_descent", "BAW123", "", 0.95},
		{"Speedbird one two tree we made a left turn heading two four zero", "readback_heading", "BAW123", "240", 0.95},
		{"Lufthansa four echo kilo requests climb flight level tree seven zero", "request_climb", "DLH4EK", "FL370", 0.95},
		{"Speedbird one two tree runway vacate", "report_vacated", "BAW123", "", 0.95},
		{"mayday mayday mayday Speedbird one two tree smoking the cockpit", "mayday", "BAW123", "smoke", 0.95},
		{"Ryanair eight nier alpha bravo descending flight level one zero zero", "readback_altitude", "RYR89AB", "FL100", 0.95},
		{"Oscar kilo alpha bravo charlie taxi to stand four two via bravo an alpha", "readback_taxi", "OK-ABC", "B A", 0.95},
		{"squawk four tree two one speed bird one two tree", "readback_squawk", "BAW123", "4321", 0.85},
		// "an" is "and" only between taxiway letters.
		{"Speedbird one two tree an engine failure mayday mayday mayday", "mayday", "BAW123", "engine_failure", 0.95},
		// Realism first: the number is never guessed, nor a far-off name.
		{"lafansa four echo lima heading two four zero", "readback_heading", "", "240", 0.6},
		{"bore one two three descending flight level one zero zero", "readback_altitude", "", "FL100", 0.6},
	} {
		r := p.Parse(c.say)
		if r.Tags[voicegoio.TagIntent] != c.intent || r.Tags[voicegoio.TagCallsign] != c.callsign ||
			r.Tags[voicegoio.TagValue] != c.value || r.Confidence != c.conf {
			t.Errorf("%q: got %v conf %.2f, want %s %s %s %.2f", c.say, r.Tags, r.Confidence, c.intent, c.callsign, c.value, c.conf)
		}
	}
}

func TestLooseCallsignNotGuessedBetweenTwo(t *testing.T) {
	p := NewParser()
	p.roster = map[string]string{"lufthansa one two": "DLH12", "lufthanza one two": "XXX12"}
	if icao, _ := p.matchCallsignLoose(tokenize("lufthansb one two")); icao != "" {
		t.Errorf("two aircraft alike: got %s, want none", icao)
	}
	if icao, _ := p.matchCallsignLoose(tokenize("lufthanza one two")); icao != "" {
		t.Errorf("exact for one, near for the other: loose gives %s, want none (exact match is matchCallsign's)", icao)
	}
}

func TestControllerClearSlip(t *testing.T) {
	p := NewControllerParser()
	p.SetCallsigns([]voicegoio.Callsign{{ICAO: "DLH4EK"}})
	for say, want := range map[string]string{
		"Lufthansa four echo kilo runway two four clear to land wind two three zero degrees one two knots": "land",
		"Lufthansa four echo kilo runway two four clear for takeoff":                                       "takeoff",
	} {
		r := p.Parse(say)
		if r.Tags[voicegoio.TagIntent] != want || r.Tags[voicegoio.TagCallsign] != "DLH4EK" || r.Tags[voicegoio.TagValue] != "24" {
			t.Errorf("%q: got %v, want %s DLH4EK 24", say, r.Tags, want)
		}
	}
}
