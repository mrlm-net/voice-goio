package say

import (
	"strings"
	"testing"

	voicegoio "github.com/mrlm-net/voice-goio"
)

// A multi speaker model must map onto voices the model actually contains.
// For en_GB-vctk-medium that is the Commonwealth spread VCTK documents —
// including Indian and South African — but never German or Italian, which no
// VCTK speaker is.
func TestVoiceStaysInItsAccentFamily(t *testing.T) {
	vctk := map[string]bool{"Daniel": true, "Moira": true, "Rishi": true, "Karen": true, "Tessa": true}
	for _, id := range []int{0, 1, 17, 42, 71, 78, 108} {
		got := voiceFor(Options{}, voicegoio.VoiceProfile{Model: "en_GB-vctk-medium", SpeakerID: id})
		if !vctk[got] {
			t.Errorf("en_GB-vctk-medium speaker %d mapped to %q, which VCTK does not contain", id, got)
		}
	}
	if got := voiceFor(Options{}, voicegoio.VoiceProfile{Model: "cs_CZ-jirka-medium"}); got != "Zuzana" {
		t.Errorf("Czech model mapped to %q, want Zuzana", got)
	}
	if got := voiceFor(Options{}, voicegoio.VoiceProfile{Model: "en_US-ryan-medium"}); got != "Ralph" {
		t.Errorf("en_US-ryan-medium mapped to %q, want the explicit Ralph mapping", got)
	}
	if got := voiceFor(Options{Voice: "Fred"}, voicegoio.VoiceProfile{Model: "en_GB-vctk-medium"}); got != "Fred" {
		t.Errorf("explicit override ignored: got %q", got)
	}
}

// A multi speaker model should still produce more than one voice.
func TestMultiSpeakerModelVaries(t *testing.T) {
	seen := map[string]bool{}
	for id := range 8 {
		seen[voiceFor(Options{}, voicegoio.VoiceProfile{Model: "en_GB-vctk-medium", SpeakerID: id})] = true
	}
	if len(seen) < 2 {
		t.Errorf("eight speakers produced %d distinct voices", len(seen))
	}
}

// LengthScale is piper's duration multiplier, so the words-per-minute rate is
// its reciprocal, clamped to what these voices stay intelligible at.
func TestRateFromLengthScale(t *testing.T) {
	slow := rateFor(Options{}, voicegoio.VoiceProfile{LengthScale: 0.95})
	fast := rateFor(Options{}, voicegoio.VoiceProfile{LengthScale: 0.80})
	if fast <= slow {
		t.Errorf("a lower length scale must speak faster: %d vs %d wpm", fast, slow)
	}
	if fast > 195 {
		t.Errorf("rate %d wpm is past the point these voices smear", fast)
	}
	if base := rateFor(Options{}, voicegoio.VoiceProfile{}); base != 165 {
		t.Errorf("default rate = %d, want 165", base)
	}
}

func TestCatalogueCoversAccents(t *testing.T) {
	accents := map[string]bool{}
	for _, v := range Catalogue {
		accents[v.Accent] = true
	}
	if len(accents) < 10 {
		t.Errorf("catalogue covers %d accents, want at least 10", len(accents))
	}
	for key, names := range accentFamilies {
		if len(names) == 0 {
			t.Errorf("accent family %q is empty", key)
		}
		for _, n := range names {
			if !strings.Contains(catalogueNames(), n) {
				t.Errorf("family %q references %q, which is not in the catalogue", key, n)
			}
		}
	}
}

func catalogueNames() string {
	var b strings.Builder
	for _, v := range Catalogue {
		b.WriteString(v.Name)
		b.WriteByte(' ')
	}
	return b.String()
}
