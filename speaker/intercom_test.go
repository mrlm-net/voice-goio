package speaker

import (
	"testing"
	"time"

	voicegoio "github.com/mrlm-net/voice-goio"
)

var alan = voicegoio.VoiceProfile{Model: "en_GB-alan-medium", SpeakerID: 0, Accent: "en-GB"}

func intercom(text string) Utterance {
	v := alan
	return Utterance{Intercom: true, Position: "copilot", Text: text, Voice: &v}
}

// The intercom speaks while the radio is off and without a frequency, on its
// own queue, without the radio chain, in the voice asked for.
func TestIntercomWithoutRadio(t *testing.T) {
	r := newRig(t, fast, Options{})
	r.s.Hear(intercom("Flaps one"))
	eventually(t, "said", func() bool { return r.player(0) != nil && len(r.player(0).got()) == 1 })
	got := r.player(0).got()[0]
	if got.queue != IntercomKey || got.who != "copilot" || got.text != "Flaps one" {
		t.Errorf("played %+v", got)
	}
	if want := len(SpokenEnd("Flaps one")) + int(TailPad.Seconds()*fakeRate); got.samples != want {
		t.Errorf("%d samples, want %d", got.samples, want)
	}
	if n := r.chain.applied(); n != 0 {
		t.Errorf("radio chain applied %d times", n)
	}
	if v := r.tts.voicesUsed(); len(v) != 1 || v[0].Model != alan.Model || v[0].SpeakerID != alan.SpeakerID {
		t.Errorf("voice %+v", v)
	}
	if st := r.s.State(); st.On || st.Status != "off" {
		t.Errorf("radio turned on: %+v", st)
	}
}

// The intercom is not tied to the frequency: any (or none) is heard, a
// change of frequency or the radio going off leaves it be.
func TestIntercomIgnoresFrequency(t *testing.T) {
	r := newRig(t, fast, Options{})
	r.s.Set(true, "118.105") // player 0: the radio
	u := intercom("Before start checklist")
	u.Frequency = "121.500"
	r.s.Hear(u) // player 1: the intercom
	eventually(t, "said", func() bool { return r.player(1) != nil && len(r.player(1).got()) == 1 })
	if len(r.player(0).got()) != 0 {
		t.Error("intercom on the radio's player")
	}
	r.s.Set(true, "120.530")
	r.s.Set(false, "")
	if !r.player(0).isClosed() || r.player(1).isClosed() {
		t.Fatal("the frequency closed the intercom (or not the radio)")
	}
	r.s.Hear(intercom("Complete"))
	eventually(t, "said after", func() bool { return len(r.player(1).got()) == 2 })
}

// One at a time, IntercomGap apart; SayIntercom and SayOnce queue on it.
func TestIntercomOneAtATime(t *testing.T) {
	r := newRig(t, fast, Options{})
	if !r.s.SayIntercom("Before start checklist", alan) {
		t.Fatal("not said")
	}
	if !r.s.SayOnce(intercom("Complete")) {
		t.Fatal("not said")
	}
	eventually(t, "both", func() bool { return r.player(0) != nil && len(r.player(0).got()) == 2 })
	got := r.player(0).got()
	first := time.Duration(got[0].samples) * time.Second / fakeRate
	if d := got[1].at.Sub(got[0].at); d < first+fast.icGap {
		t.Errorf("second after %v, want at least %v (said) + %v (gap)", d, first, fast.icGap)
	}
	if got[0].queue != IntercomKey || got[1].queue != IntercomKey {
		t.Errorf("queues %q, %q", got[0].queue, got[1].queue)
	}
}

// Without a voice SayIntercom says so; after Close nothing reopens.
func TestIntercomNoVoice(t *testing.T) {
	r := newRig(t, fast, Options{})
	r.s.openEngine = func() (*engine, error) { return nil, ErrNoVoice }
	if r.s.SayIntercom("Flaps one", alan) {
		t.Error("said without a voice")
	}
	if st := r.s.State(); st.Status != ErrNoVoice.Error() {
		t.Errorf("status %q", st.Status)
	}
	r2 := newRig(t, fast, Options{})
	r2.s.Close()
	if r2.s.SayIntercom("Flaps one", alan) || r2.player(0) != nil {
		t.Error("said after Close")
	}
}

// An explicit voice wins over the pool's pick on the radio too; its Radio
// left "" is the position's (center for a pilot).
func TestExplicitVoice(t *testing.T) {
	r := newRig(t, fast, Options{})
	e, _ := r.s.openEngine()
	v := voicegoio.VoiceProfile{Model: "en_US-ryan-medium", SpeakerID: 3}
	u := Utterance{Airport: "LKPR", Position: PosGround, Voice: &v}
	if got := r.s.voiceOf(e, u); got.Model != v.Model || got.SpeakerID != 3 || got.Radio != string(voicegoio.Ground) {
		t.Errorf("ground %+v", got)
	}
	u.Pilot, u.Callsign = true, "CSA123"
	if got := r.s.voiceOf(e, u); got.Model != v.Model || got.Radio != string(voicegoio.Center) {
		t.Errorf("pilot %+v", got)
	}
	v.Radio = "approach"
	if got := r.s.voiceOf(e, u); got.Radio != "approach" {
		t.Errorf("radio %+v", got)
	}
	if v.Radio != "approach" || u.Voice.Radio != "approach" {
		t.Error("caller's profile changed")
	}
	// Said on the radio: in that voice, through the chain.
	r.s.Set(true, "118.105")
	h := tower("CSA123, taxi to holding point A")
	h.Voice = &voicegoio.VoiceProfile{Model: "en_GB-vctk-medium", SpeakerID: 42}
	r.s.Hear(h)
	eventually(t, "said", func() bool { return len(r.player(0).got()) == 1 })
	if got := r.tts.voicesUsed(); got[0].Model != "en_GB-vctk-medium" || got[0].SpeakerID != 42 || got[0].Radio != string(voicegoio.Tower) {
		t.Errorf("voice %+v", got[0])
	}
	if r.chain.applied() != 1 {
		t.Errorf("chain applied %d times", r.chain.applied())
	}
	// An ATIS clip with a voice is in that voice, not the ATIS pick.
	if _, _, err := r.s.Clip(Utterance{Airport: "LKPR", Position: PosATIS, Text: "x", Voice: &v}); err != nil {
		t.Fatal(err)
	}
	if got := r.tts.voicesUsed(); got[1].Model != v.Model {
		t.Errorf("atis clip voice %+v", got[1])
	}
}

// An intercom clip is dry: no radio chain.
func TestClipIntercom(t *testing.T) {
	r := newRig(t, fast, Options{})
	pcm, rate, err := r.s.Clip(intercom("Gear up"))
	if err != nil || rate != fakeRate || len(pcm) != len("Gear up") {
		t.Fatalf("clip %d samples at %d: %v", len(pcm), rate, err)
	}
	if r.chain.applied() != 0 {
		t.Error("radio chain on the intercom")
	}
	if _, _, err := r.s.Clip(tower("roger")); err != nil || r.chain.applied() != 1 {
		t.Errorf("radio clip: chain %d, %v", r.chain.applied(), err)
	}
}

// SetDevice moves the intercom too.
func TestIntercomSetDevice(t *testing.T) {
	r := newRig(t, fast, Options{})
	r.s.SayIntercom("Flaps one", alan)
	eventually(t, "opened", func() bool { return r.player(0) != nil })
	if err := r.s.SetDevice("1"); err != nil {
		t.Fatal(err)
	}
	if r.player(0).dev() != "1" {
		t.Errorf("intercom on %q", r.player(0).dev())
	}
}
