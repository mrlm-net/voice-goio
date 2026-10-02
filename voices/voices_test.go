package voices_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/voices"
)

func newPool(t *testing.T) *voices.Pool {
	t.Helper()
	m, err := voices.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	// An empty folder: nothing installed, so every model counts and the
	// tests do not depend on what this machine has downloaded.
	return voices.NewPool(m, voices.PoolOptions{Dir: t.TempDir(), Seed: 1, AllowUnaudited: true})
}

// Only installed voices are assigned: an airport whose voices are not
// downloaded was silent (the speaker logged "not found" and said nothing).
// With one model in the folder, every position and the ATIS get it.
func TestAssignsOnlyInstalledVoices(t *testing.T) {
	m, err := voices.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	model, ok := m.Model("en_GB-alan-medium")
	if !ok {
		t.Fatal("en_GB-alan-medium not in the manifest")
	}
	dir := t.TempDir()
	onnx := filepath.Join(dir, filepath.FromSlash(model.ONNX))
	if err := os.MkdirAll(filepath.Dir(onnx), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(onnx, []byte("onnx"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := voices.NewPool(m, voices.PoolOptions{Dir: dir, Seed: 1, AllowUnaudited: true})
	for _, k := range []voicegoio.ControllerKind{voicegoio.Tower, voicegoio.Ground, voicegoio.ATIS} {
		if v := p.Assign("LKPR", k); v.Model != model.Name {
			t.Errorf("%s at LKPR: %q, want the installed %q", k, v.Model, model.Name)
		}
	}
}

// The quality bar in SPEC.md is BeyondATC's free "Basic" tier: about 100 local
// neural voices with accents. The manifest has to be able to reach that before
// any of it is downloaded, so this asserts the pool's own arithmetic.
func TestPoolMeetsTheBeyondATCBasicBar(t *testing.T) {
	p := newPool(t)
	if n := p.Count(); n < 100 {
		t.Errorf("assignable voices = %d, want at least 100", n)
	}
	accents := p.Accents()
	if len(accents) < 10 {
		t.Errorf("distinct accents = %d (%v), want at least 10", len(accents), accents)
	}
	t.Logf("%d voices across %d accents: %v", p.Count(), len(accents), accents)
}

// A controller keeps one voice for the session, and no two positions at the
// same airport share a speaker: hearing tower and ground in the same voice
// breaks the illusion faster than any accent mismatch.
func TestAssignmentIsStableAndUnique(t *testing.T) {
	p := newPool(t)
	first := p.Assign("LKPR", voicegoio.Tower)
	if again := p.Assign("LKPR", voicegoio.Tower); again != first {
		t.Errorf("tower voice changed within a session: %+v then %+v", first, again)
	}

	seen := map[string]bool{}
	for _, k := range []voicegoio.ControllerKind{
		voicegoio.Ground, voicegoio.Tower, voicegoio.Approach, voicegoio.Center, voicegoio.ATIS,
	} {
		v := p.Assign("LKPR", k)
		key := v.Model + "#" + string(rune(v.SpeakerID))
		if seen[key] {
			t.Errorf("%s reused voice %s speaker %d at LKPR", k, v.Model, v.SpeakerID)
		}
		seen[key] = true
		if v.Radio != string(k) {
			t.Errorf("%s got radio profile %q", k, v.Radio)
		}
		if v.LengthScale < 0.80 || v.LengthScale > 0.95 {
			t.Errorf("%s length scale %v out of the 0.80-0.95 range", k, v.LengthScale)
		}
	}
}

// Two runs with the same seed must produce the same session: a recorded flight
// should sound the same when it is replayed.
func TestAssignmentIsDeterministic(t *testing.T) {
	a, b := newPool(t), newPool(t)
	for _, icao := range []string{"LKPR", "EGLL", "KJFK", "EDDF"} {
		if x, y := a.Assign(icao, voicegoio.Tower), b.Assign(icao, voicegoio.Tower); x != y {
			t.Errorf("%s: %+v != %+v", icao, x, y)
		}
	}
}

// Region weighting is the reason a Prague controller sounds Czech and a London
// one does not.
func TestRegionWeighting(t *testing.T) {
	m, err := voices.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	p := voices.NewPool(m, voices.PoolOptions{Seed: 7, AllowUnaudited: true})

	cz := p.Assign("LKPR", voicegoio.Tower)
	uk := p.Assign("EGLL", voicegoio.Tower)
	us := p.Assign("KJFK", voicegoio.Tower)

	if cz.Model != "cs_CZ-jirka-medium" {
		t.Errorf("LKPR tower got %s, want the Czech accented model first", cz.Model)
	}
	if uk.Model == cz.Model {
		t.Errorf("EGLL and LKPR both got %s", uk.Model)
	}
	if !strings.HasPrefix(us.Model, "en_US") {
		t.Errorf("KJFK tower got %s, want a US model", us.Model)
	}
	t.Logf("LKPR=%s EGLL=%s KJFK=%s", cz.Model, uk.Model, us.Model)
}

// Licence filtering is a build flag rather than a setting, so a shipped binary
// cannot be configured into a breach. Here we only check the mechanism.
func TestCommercialFilterExcludesNonPermissive(t *testing.T) {
	m, err := voices.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	open := voices.NewPool(m, voices.PoolOptions{Seed: 1, AllowUnaudited: true})
	strict := voices.NewPool(m, voices.PoolOptions{Seed: 1, AllowUnaudited: true, CommercialOnly: true})
	if strict.Count() > open.Count() {
		t.Errorf("commercial pool (%d) is larger than the open pool (%d)", strict.Count(), open.Count())
	}
	if strict.Count() == 0 {
		t.Error("commercial filtering excluded every voice")
	}
}

func TestMissingReportsUndownloadedModels(t *testing.T) {
	m, err := voices.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	p := voices.NewPool(m, voices.PoolOptions{Dir: t.TempDir()})
	if len(p.Missing()) != len(m.Models) {
		t.Errorf("Missing() = %d, want all %d models", len(p.Missing()), len(m.Models))
	}
}

// An ATIS is a machine reading a template on a loop, not a controller. It gets
// one fixed flat voice everywhere, it does not consume an airport's pool, and
// giving Heathrow a different ATIS reader from Gatwick would be wrong rather
// than varied.
func TestATISVoiceIsFixedEverywhere(t *testing.T) {
	p := newPool(t)
	first := p.Assign("EGLL", voicegoio.ATIS)
	if first.Model == "" {
		t.Fatal("ATIS got no model")
	}
	if first.Accent != voices.AtisAccent {
		t.Errorf("ATIS accent = %q, want %q", first.Accent, voices.AtisAccent)
	}
	if first.Radio != string(voicegoio.ATIS) {
		t.Errorf("ATIS radio profile = %q", first.Radio)
	}
	for _, icao := range []string{"EGKK", "KJFK", "LKPR", "VIDP"} {
		if got := p.Assign(icao, voicegoio.ATIS); got != first {
			t.Errorf("%s ATIS = %+v, want the same fixed voice %+v", icao, got, first)
		}
	}

	// It must not have reserved a speaker: a controller at the same airport is
	// still free to use that voice.
	q := newPool(t)
	q.Assign("EGLL", voicegoio.ATIS)
	tower := q.Assign("EGLL", voicegoio.Tower)
	other := newPool(t).Assign("EGLL", voicegoio.Tower)
	if tower != other {
		t.Errorf("assigning the ATIS changed the tower voice: %+v vs %+v", tower, other)
	}
}

// An excluded model is never assigned, even where its accent fits best
// (LKPR prefers the Czech accent).
func TestExcludedVoicesAreNotAssigned(t *testing.T) {
	m, err := voices.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	p := voices.NewPool(m, voices.PoolOptions{Dir: t.TempDir(), Seed: 1, AllowUnaudited: true, Exclude: []string{"cs_CZ-jirka-medium"}})
	for _, k := range []voicegoio.ControllerKind{voicegoio.Tower, voicegoio.Ground, voicegoio.Approach} {
		if v := p.Assign("LKPR", k); v.Model == "cs_CZ-jirka-medium" {
			t.Errorf("%s at LKPR got the excluded %s", k, v.Model)
		}
	}
}

// FemaleShare: about one position in nine gets a female voice (1:8), the
// rest a male one, from the speakers whose gender the manifest documents.
func TestFemaleShare(t *testing.T) {
	m, err := voices.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	p := voices.NewPool(m, voices.PoolOptions{Dir: t.TempDir(), Seed: 3, AllowUnaudited: true, FemaleShare: 1.0 / 9})
	gender := func(v voicegoio.VoiceProfile) string {
		model, ok := m.Model(v.Model)
		if !ok {
			return ""
		}
		for _, s := range model.Speakers {
			if s.ID == v.SpeakerID {
				return s.Gender
			}
		}
		return ""
	}
	female, total := 0, 0
	for i := range 300 {
		for _, kind := range []voicegoio.ControllerKind{voicegoio.Ground, voicegoio.Tower, voicegoio.Approach} {
			v := p.Assign(fmt.Sprintf("K%03d", i), kind)
			total++
			if gender(v) == "F" {
				female++
			}
		}
	}
	share := float64(female) / float64(total)
	if share < 0.07 || share > 0.16 {
		t.Errorf("%d of %d positions female (%.0f%%), want about 1 in 9", female, total, share*100)
	}
	t.Logf("%d of %d positions female (%.1f%%)", female, total, share*100)
}
