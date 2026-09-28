package fake_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/stt/fake"
)

const corpus = "../../testdata/stt/readbacks.jsonl"

func loadCorpus(t *testing.T) []fake.ScriptLine {
	t.Helper()
	f, err := os.Open(corpus)
	if err != nil {
		t.Fatalf("open corpus: %v", err)
	}
	defer f.Close()
	lines, err := fake.ReadScript(f)
	if err != nil {
		t.Fatal(err)
	}
	return lines
}

// The corpus is the shared contract between the Go parser and the SAPI grammar.
// SPEC.md 9 requires >= 95% correct intent and callsign; the Go parser is the
// reference, so it must be at 100%.
func TestParserAgainstCorpus(t *testing.T) {
	lines := loadCorpus(t)
	p := fake.NewParser()
	var total, ok int
	for _, l := range lines {
		if len(l.Callsigns) > 0 {
			cs := make([]voicegoio.Callsign, 0, len(l.Callsigns))
			for _, id := range l.Callsigns {
				cs = append(cs, voicegoio.Callsign{ICAO: id})
			}
			p.SetCallsigns(cs)
			continue
		}
		total++
		got := p.Parse(l.Text)
		bad := false
		for _, key := range []string{voicegoio.TagIntent, voicegoio.TagCallsign, voicegoio.TagValue, voicegoio.TagReason} {
			want := l.Tags[key]
			if got.Tags[key] != want {
				t.Errorf("%q\n  %s = %q, want %q", l.Text, key, got.Tags[key], want)
				bad = true
			}
		}
		if !bad {
			ok++
		}
	}
	t.Logf("%d/%d phrases tagged exactly", ok, total)
	if total == 0 {
		t.Fatal("corpus is empty")
	}
}

// Anything outside the grammar has to come back as say_again rather than as a
// confident wrong answer: the application turns say_again into "say again" on
// the radio, which is the safe failure.
func TestOffGrammarIsSayAgain(t *testing.T) {
	p := fake.NewParser()
	p.SetCallsigns([]voicegoio.Callsign{{ICAO: "BAW123"}})
	for _, s := range []string{
		"open the pod bay doors",
		"",
		"zzzzz",
		"Speedbird one two three",
	} {
		got := p.Parse(s)
		if got.Tags[voicegoio.TagIntent] != voicegoio.IntentSayAgain {
			t.Errorf("Parse(%q) intent = %q, want say_again", s, got.Tags[voicegoio.TagIntent])
		}
	}
}

// A callsign that is not on frequency must not be invented.
func TestCallsignRestrictedToRoster(t *testing.T) {
	p := fake.NewParser()
	p.SetCallsigns([]voicegoio.Callsign{{ICAO: "BAW123"}})
	got := p.Parse("Lufthansa four echo kilo descending flight level one zero zero")
	if cs := got.Tags[voicegoio.TagCallsign]; cs != "" {
		t.Errorf("callsign = %q, want empty for an aircraft not on frequency", cs)
	}
	if got.Confidence >= 0.95 {
		t.Errorf("confidence = %v, want a low value without a callsign", got.Confidence)
	}
}

func TestScriptRecognizer(t *testing.T) {
	r, err := fake.FromScript(filepath.Clean(corpus))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
	rec := <-r.Results()
	if rec.Tags[voicegoio.TagCallsign] != "BAW123" {
		t.Errorf("first scripted recognition: %+v", rec.Tags)
	}
	if r.Remaining() == 0 {
		t.Error("script should have lines left")
	}
}

// A recogniser reading from a closed input must close its results channel:
// otherwise an application looping on Results deadlocks when stdin ends, which
// is exactly what a piped demo run does.
func TestReaderClosesResultsAtEOF(t *testing.T) {
	r := fake.FromReader(strings.NewReader("Speedbird one two three wilco\n"))
	defer r.Close()
	if err := r.SetCallsigns([]voicegoio.Callsign{{ICAO: "BAW123"}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := <-r.Results(); !ok {
		t.Fatal("first receive should have delivered a recognition")
	}
	select {
	case _, ok := <-r.Results():
		if ok {
			t.Error("expected the channel to be closed after the input ended")
		}
	case <-time.After(2 * time.Second):
		t.Error("results channel was not closed at EOF")
	}
}

func TestReaderRecognizer(t *testing.T) {
	r := fake.FromReader(strings.NewReader("Speedbird one two three wilco\n"))
	defer r.Close()
	if err := r.SetCallsigns([]voicegoio.Callsign{{ICAO: "BAW123"}}); err != nil {
		t.Fatal(err)
	}
	rec := <-r.Results()
	if rec.Tags[voicegoio.TagIntent] != "wilco" {
		t.Errorf("tags = %+v", rec.Tags)
	}
}
