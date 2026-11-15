package fake_test

import (
	"encoding/xml"
	"maps"
	"os"
	"regexp"
	"slices"
	"testing"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/grammar"
	"github.com/mrlm-net/voice-goio/stt/fake"
)

const controllerCorpus = "../../testdata/stt/controller.jsonl"

// The controller parser tags every corpus line exactly: all the tags, none
// more (#48).
func TestControllerParserAgainstCorpus(t *testing.T) {
	f, err := os.Open(controllerCorpus)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	lines, err := fake.ReadScript(f)
	if err != nil {
		t.Fatal(err)
	}
	p := fake.NewControllerParser()
	n := 0
	for _, l := range lines {
		if len(l.Callsigns) > 0 {
			var cs []voicegoio.Callsign
			for _, id := range l.Callsigns {
				cs = append(cs, voicegoio.Callsign{ICAO: id})
			}
			p.SetCallsigns(cs)
			continue
		}
		n++
		if got := p.Parse(l.Text).Tags; !maps.Equal(got, l.Tags) {
			t.Errorf("%q\n  got  %v\n  want %v", l.Text, got, l.Tags)
		}
	}
	if n < 25 {
		t.Errorf("%d corpus lines", n)
	}
}

// The grammar is well-formed XML and says every intent the corpus expects,
// so the SAPI backend can produce what the parser does.
func TestControllerGrammarIntents(t *testing.T) {
	if err := xml.Unmarshal([]byte(grammar.Controller), new(struct{ XMLName xml.Name })); err != nil {
		t.Fatalf("not XML: %v", err)
	}
	said := map[string]bool{}
	for _, m := range regexp.MustCompile(`out(?:\.intent)?="([a-z_]+)"`).FindAllStringSubmatch(grammar.Controller, -1) {
		said[m[1]] = true
	}
	f, err := os.Open(controllerCorpus)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	lines, _ := fake.ReadScript(f)
	want := map[string]bool{}
	for _, l := range lines {
		if in := l.Tags[voicegoio.TagIntent]; in != "" {
			want[in] = true
		}
	}
	for _, in := range slices.Sorted(maps.Keys(want)) {
		if !said[in] {
			t.Errorf("intent %q not in controller.grxml", in)
		}
	}
	for _, in := range []string{"startup", "pushback", "pushstart", "taxi", "upto", "hold", "cross", "handoff",
		"lineup", "lineupbehind", "takeoff", "abort", "land", "goaround", "vacate"} {
		if !want[in] {
			t.Errorf("engine verb %q not in the corpus", in)
		}
	}
}

// A script recogniser switched to the controller grammar tags controller
// speech with the roster it had.
func TestRecognizerControllerGrammar(t *testing.T) {
	r := fake.FromLines([]fake.ScriptLine{{Callsigns: []string{"BAW123"}}, {Text: "Speedbird one two three cross runway one two"}})
	r.UseControllerGrammar()
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
	got := <-r.Results()
	if got.Tags[voicegoio.TagIntent] != "cross" || got.Tags[voicegoio.TagCallsign] != "BAW123" || got.Tags[voicegoio.TagValue] != "12" {
		t.Errorf("tags %v", got.Tags)
	}
	r.Close()
}
