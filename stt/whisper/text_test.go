package whisper

import (
	"testing"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/normalise"
)

func TestSpoken(t *testing.T) {
	for in, want := range map[string]string{
		"Speedbird 123, runway 24, cleared to land.":      "speedbird one two three runway two four cleared to land",
		"Contact tower 118.1.":                            "contact tower one one eight decimal one",
		"Climb FL350":                                     "climb flight level three five zero",
		"Taxi to holding point A1 via B, A.":              "taxi to holding point alpha one via bravo alpha",
		"Cleared for take-off, wind 240 degrees, 8 knots": "cleared for take off wind two four zero degrees eight knots",
		"Expect a delay":                                  "expect a delay",
		"  ":                                              "",
	} {
		if got := Spoken(in); got != want {
			t.Errorf("%q\n  got  %q\n  want %q", in, got, want)
		}
	}
}

func TestWritten(t *testing.T) {
	cs := []voicegoio.Callsign{{ICAO: "DLH4EK"}, {ICAO: "CSA1234"}, {ICAO: "BAW123", Spoken: "Speedbird one two three"}, {ICAO: "OK-ABC"}}
	for in, want := range map[string]string{
		"DLH4EK turn left heading 240.":           normalise.SpokenCallsign("DLH4EK") + " turn left heading 240.",
		"CSA 1234 with you, 15,000.":              normalise.SpokenCallsign("CSA1234") + " with you, 15000.",
		"Heathrow Centre, BAW 123/15000 with you": "Heathrow Centre, Speedbird one two three/15000 with you",
		"ok abc ready":                            normalise.SpokenCallsign("OK-ABC") + " ready",
		"heading 123":                             "heading 123",
	} {
		if got := Written(in, cs); got != want {
			t.Errorf("%q\n  got  %q\n  want %q", in, got, want)
		}
	}
}
