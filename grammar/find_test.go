package grammar

import "testing"

func TestFind(t *testing.T) {
	cmds := []Command{
		{Intent: "request_taxi", Phrases: []string{"request taxi", "ready to taxi"}},
		{Intent: "request_pushback", Phrases: []string{"request pushback", "request push and start"}},
		{Intent: "ready_for_departure", Phrases: []string{"ready for departure"}},
		{Intent: "answer", Phrases: []string{"checked", "on", "zero", "flaps one"}},
		{Intent: "a", Phrases: []string{"request startup"}},
		{Intent: "b", Phrases: []string{"request start up now"}},
		{Intent: "c", Phrases: []string{"request startup"}},
	}
	for text, want := range map[string]string{
		"Prague Ground, OK-ABC, request taxi.":                      "request_taxi",
		"Prague Ground OK ABC request pushback":                     "request_pushback",
		"Ground, Speedbird one two three, request pushbak":          "request_pushback",
		"Speedbird one two three request push and start, stand C24": "request_pushback",
		"Tower, CSA Lines one two tree four, ready for departure":   "ready_for_departure",
		"Checked.":                            "answer",
		"flaps one":                           "answer",
		"on":                                  "answer",
		"climbing flight level one zero zero": "",
		"Speedbird one two tree on frequency": "",
		"Ground, request startup":             "", // a and c: two intents, not guessed
		"request taxes":                       "", // short word misheard: not accepted
		"rekwest pushbak":                     "", // no word exact
		"":                                    "",
	} {
		got, ok := Find(cmds, text)
		if got != want || ok != (want != "") {
			t.Errorf("%q: got %q %v, want %q", text, got, ok, want)
		}
	}
}
