package whisper

import "testing"

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
