package speaker

import (
	"testing"
	"time"
)

// A call chime plays on the intercom in order, before what is said after
// it, and the answer waits for the pickup; no voice is synthesised for it.
func TestChimeBeforeIntercom(t *testing.T) {
	r := newRig(t, fast, Options{})
	if !r.s.Chime(ChimeCall) || !r.s.SayIntercom("Captain speaking", alan) {
		t.Fatal("not queued")
	}
	eventually(t, "both played", func() bool { return r.player(0) != nil && len(r.player(0).got()) == 2 })
	got := r.player(0).got()
	if got[0].who != "chime" || got[0].samples != len(ChimePCM(ChimeCall)) || got[1].text != "Captain speaking" {
		t.Fatalf("played %+v", got)
	}
	chime := time.Duration(float64(len(ChimePCM(ChimeCall))) / ChimeRate * float64(time.Second))
	if gap := got[1].at.Sub(got[0].at); gap < chime+fast.pickup-50*time.Millisecond {
		t.Errorf("answered %v after the call, want the chime and a pickup (%v+)", gap, chime+fast.pickup)
	}
	if v := r.tts.voicesUsed(); len(v) != 1 {
		t.Errorf("%d syntheses, want the speech only", len(v))
	}
}

// The chimes are made in code; Clip renders one; an unknown is refused.
func TestChimePCM(t *testing.T) {
	for _, c := range []Chime{ChimeCall, ChimePA, ChimeSeatbelt} {
		pcm := ChimePCM(c)
		if len(pcm) < ChimeRate/2 {
			t.Errorf("%s: %d samples", c, len(pcm))
		}
		var peak int16
		for _, v := range pcm {
			peak = max(peak, v)
		}
		if peak < 3000 || peak == 32767 {
			t.Errorf("%s: peak %d", c, peak)
		}
	}
	r := newRig(t, fast, Options{})
	if pcm, rate, err := r.s.Clip(Utterance{Chime: ChimePA}); err != nil || rate != ChimeRate || len(pcm) != len(ChimePCM(ChimePA)) {
		t.Errorf("clip: %d samples at %d, %v", len(pcm), rate, err)
	}
	if r.s.Chime("gong") {
		t.Error("an unknown chime queued")
	}
}
