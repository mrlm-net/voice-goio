package grammar

import (
	"encoding/xml"
	"strings"
	"testing"
)

var crew = []Command{
	{Intent: "request_taxi", Phrases: []string{"request taxi", "ready to taxi"}},
	{Intent: "gear_up", Phrases: []string{"Gear up.", "landing gear up"}},
	{Intent: "doors_closed", Phrases: []string{"doors closed", "close the doors & let's go"}},
}

// srgs is the part of SRGS the test reads back.
type srgs struct {
	XMLName xml.Name `xml:"grammar"`
	Root    string   `xml:"root,attr"`
	Rules   []struct {
		ID    string `xml:"id,attr"`
		Scope string `xml:"scope,attr"`
		Items []struct {
			Text string `xml:",chardata"`
			Tag  string `xml:"tag"`
		} `xml:"one-of>item"`
	} `xml:"rule"`
}

// The grammar is SRGS as the ATC one: root "transmission" with an intent per
// phrase, a public "callsign" rule for SetCallsigns, the words escaped.
func TestCommands(t *testing.T) {
	g, err := Commands(crew)
	if err != nil {
		t.Fatal(err)
	}
	var doc srgs
	if err := xml.Unmarshal([]byte(g), &doc); err != nil {
		t.Fatalf("not XML: %v\n%s", err, g)
	}
	if doc.Root != "transmission" || len(doc.Rules) != 2 || doc.Rules[0].ID != "transmission" || doc.Rules[1].ID != "callsign" || doc.Rules[1].Scope != "public" {
		t.Fatalf("rules %+v", doc)
	}
	want := map[string]string{
		"request taxi": "request_taxi", "ready to taxi": "request_taxi",
		"gear up": "gear_up", "landing gear up": "gear_up",
		"doors closed": "doors_closed", "close the doors let's go": "doors_closed",
	}
	items := doc.Rules[0].Items
	if len(items) != len(want) {
		t.Fatalf("%d items, want %d", len(items), len(want))
	}
	for _, it := range items {
		text := strings.TrimSpace(it.Text)
		if intent, ok := want[text]; !ok || it.Tag != `out.intent="`+intent+`";` {
			t.Errorf("item %q tag %q", text, it.Tag)
		}
	}
	if !strings.Contains(g, `<ruleref uri="#callsign"/><tag>out.callsign=rules.callsign;</tag>`) {
		t.Error("no optional callsign before the command")
	}
}

func TestCommandsRejects(t *testing.T) {
	for name, cmds := range map[string][]Command{
		"none":         nil,
		"no phrases":   {{Intent: "x"}},
		"empty phrase": {{Intent: "x", Phrases: []string{" .!"}}},
		"quote":        {{Intent: `x";out.y="z`, Phrases: []string{"a"}}},
		"space":        {{Intent: "gear up", Phrases: []string{"gear up"}}},
		"empty intent": {{Phrases: []string{"gear up"}}},
	} {
		if _, err := Commands(cmds); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestMatch(t *testing.T) {
	for text, want := range map[string]string{
		"Gear up":                   "gear_up",
		"  landing  GEAR up!":       "gear_up",
		"request taxi.":             "request_taxi",
		"close the doors, let's go": "doors_closed",
		"gear down":                 "",
		"":                          "",
	} {
		got, ok := Match(crew, text)
		if got != want || ok != (want != "") {
			t.Errorf("%q: %q %v, want %q", text, got, ok, want)
		}
	}
}
