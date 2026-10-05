package speaker

import (
	"math"
	"testing"
	"time"
)

// A PA plays on its own player, beside the intercom (both at once), its
// ding first; each channel can have its own output.
func TestPAChannel(t *testing.T) {
	r := newRig(t, fast, Options{})
	if !r.s.SayIntercom("Cabin crew, seats for take-off", alan) || !r.s.Chime(ChimePA) || !r.s.SayPA("Ladies and gentlemen, welcome aboard", alan) {
		t.Fatal("not queued")
	}
	eventually(t, "both channels played", func() bool {
		return r.player(1) != nil && len(r.player(0).got()) >= 1 && len(r.player(1).got()) == 2
	})
	ic, pa := r.player(0).got(), r.player(1).got()
	if ic[0].queue != IntercomKey || pa[0].who != "chime" || pa[0].queue != PAKey || pa[1].text != "Ladies and gentlemen, welcome aboard" {
		t.Fatalf("intercom %+v, PA %+v", ic, pa)
	}
	if d := pa[0].at.Sub(ic[0].at); d > 500*time.Millisecond || d < -500*time.Millisecond {
		t.Errorf("the PA waited for the intercom (%v): they overlap", d)
	}
	if err := r.s.SetDeviceFor(ChannelPA, "cabin"); err != nil {
		t.Fatal(err)
	}
	if st := r.s.State(); st.DeviceFor[ChannelPA] != "cabin" {
		t.Errorf("device for %+v", st.DeviceFor)
	}
	if err := r.s.SetDeviceFor(ChannelPA, ""); err != nil || len(r.s.State().DeviceFor) != 0 {
		t.Errorf("not back to the main device: %v %+v", err, r.s.State().DeviceFor)
	}
}

// The cabin chain keeps speech, cuts what a ceiling speaker cannot play.
func TestPAChain(t *testing.T) {
	const rate = 22050
	tone := func(hz float64) []int16 {
		out := make([]int16, rate/2)
		for i := range out {
			out[i] = int16(8000 * math.Sin(2*math.Pi*hz*float64(i)/rate))
		}
		return out
	}
	rms := func(p []int16) float64 {
		s := 0.0
		for _, v := range p[len(p)/2:] {
			s += float64(v) * float64(v)
		}
		return math.Sqrt(s / float64(len(p)/2))
	}
	low, mid, high := rms(PAChain(tone(80), rate)), rms(PAChain(tone(1000), rate)), rms(PAChain(tone(8000), rate))
	if !(mid > 3*low && mid > 3*high) {
		t.Errorf("rms 80 Hz %.0f, 1 kHz %.0f, 8 kHz %.0f: not band limited", low, mid, high)
	}
}

// One voice says one line at a time across the channels: the same voice on
// the PA waits for its intercom line; another voice does not.
func TestOneVoiceOneLine(t *testing.T) {
	r := newRig(t, fast, Options{})
	other := alan
	other.SpeakerID = 7
	if !r.s.SayIntercom("Cabin crew, seats for landing", alan) || !r.s.SayPA("Ladies and gentlemen, we are landing", alan) {
		t.Fatal("not queued")
	}
	eventually(t, "both played", func() bool { return r.player(1) != nil && len(r.player(0).got()) == 1 && len(r.player(1).got()) == 1 })
	ic, pa := r.player(0).got()[0], r.player(1).got()[0]
	// Either queue may take the voice first: the other waits for its line.
	first, second := ic, pa
	if pa.at.Before(ic.at) {
		first, second = pa, ic
	}
	firstLen := time.Duration(float64(first.samples) / fakeRate * float64(time.Second))
	if second.at.Before(first.at.Add(firstLen - 30*time.Millisecond)) {
		t.Errorf("same voice: the second line started %v after the first, which lasts %v", second.at.Sub(first.at), firstLen)
	}

	r2 := newRig(t, fast, Options{})
	if !r2.s.SayIntercom("Cabin crew, seats for landing", alan) || !r2.s.SayPA("Ladies and gentlemen, we are landing", other) {
		t.Fatal("not queued")
	}
	eventually(t, "both played", func() bool { return r2.player(1) != nil && len(r2.player(0).got()) == 1 && len(r2.player(1).got()) == 1 })
	ic2, pa2 := r2.player(0).got()[0], r2.player(1).got()[0]
	if d := pa2.at.Sub(ic2.at); d > 300*time.Millisecond || d < -300*time.Millisecond {
		t.Errorf("different voices waited for each other: %v apart", d)
	}
}
