package normalise_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/normalise"
)

// TestGolden drives every case in testdata/normalise. Keeping the corpus in a
// data file rather than in Go source means a new phrase is one line, and the
// same file can be read by cmd/voicecheck to synthesise the phrases for a
// listening pass.
func TestGolden(t *testing.T) {
	cases := map[string]voicegoio.Phraseology{
		"icao.txt": voicegoio.ICAO,
		"faa.txt":  voicegoio.FAA,
	}
	n := normalise.New()
	for file, ph := range cases {
		path := filepath.Join("..", "testdata", "normalise", file)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			in, want, ok := strings.Cut(line, "|")
			if !ok {
				t.Errorf("%s:%d: malformed case %q", file, i+1, line)
				continue
			}
			in, want = strings.TrimSpace(in), strings.TrimSpace(want)
			if got := n.Spoken(in, ph); got != want {
				t.Errorf("%s:%d Spoken(%q, %s)\n got: %s\nwant: %s", file, i+1, in, ph, got, want)
			}
		}
	}
}

func TestSpokenCallsign(t *testing.T) {
	for in, want := range map[string]string{
		"BAW123": "Speedbird one two three",
		"baw123": "Speedbird one two three",
		"OK-ABC": "oscar kilo alpha bravo charlie",
		"DLH4EK": "Lufthansa four echo kilo",
		"N123AB": "november one two three alpha bravo",
		"":       "",
	} {
		if got := normalise.SpokenCallsign(in); got != want {
			t.Errorf("SpokenCallsign(%q) = %q, want %q", in, got, want)
		}
	}
}

// The recogniser must accept either phraseology for the same aircraft, so every
// spoken callsign expands into one grammar entry per digit variant.
func TestSpokenVariants(t *testing.T) {
	got := normalise.SpokenVariants("Speedbird one two three")
	want := map[string]bool{
		"Speedbird one two three": true,
		"Speedbird one two tree":  true,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d variants %q, want %d", len(got), got, len(want))
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected variant %q", g)
		}
	}
	if v := normalise.SpokenVariants("Lufthansa four echo kilo"); len(v) != 1 {
		t.Errorf("callsign without variable digits expanded to %d forms, want 1", len(v))
	}
}

// No output token may still be a digit or an abbreviation: the TTS only ever
// sees words. This is the invariant the whole package exists to uphold.
func TestNoBareDigitsEscape(t *testing.T) {
	n := normalise.New()
	inputs := []string{
		"BAW123 climb FL350 QNH 1013 SQK 4321 RWY 27L TWY A3 127.45 5000 ft 250 kt",
		"OK-ABC hold short RWY 06, contact 118.005",
	}
	for _, ph := range []voicegoio.Phraseology{voicegoio.ICAO, voicegoio.FAA} {
		for _, in := range inputs {
			for _, w := range strings.Fields(n.Spoken(in, ph)) {
				if strings.ContainsAny(w, "0123456789") {
					t.Errorf("%s: %q still contains a digit in output of %q", ph, w, in)
				}
			}
		}
	}
}

func BenchmarkSpoken(b *testing.B) {
	n := normalise.New()
	const in = "BAW123 descend FL100, QNH 1013, contact Praha Radar on 127.45"
	b.ReportAllocs()
	for range b.N {
		_ = n.Spoken(in, voicegoio.ICAO)
	}
}
