package speaker

import (
	"math"
	"time"
)

// Chime is a cabin chime played on the intercom, in order with what is said
// there: the call before a cabin ↔ flight deck exchange, the ding before a
// PA. Made in code: no files.
type Chime string

const (
	ChimeCall     Chime = "call"     // the cabin call: two tones, high then low ("ding-dong"), about 1.4 s
	ChimePA       Chime = "pa"       // a single soft "ding" before a passenger announcement
	ChimeSeatbelt Chime = "seatbelt" // the seat-belt sign's single low "bong"
)

// ChimeRate is the sample rate of the chimes.
const ChimeRate = 22050

// Pickup is how long the next intercom item waits after a ChimeCall: the
// one called picks up (PickupMin plus up to PickupJitter).
const (
	PickupMin    = 1500 * time.Millisecond
	PickupJitter = 1500 * time.Millisecond
)

// ChimePCM is c as 16-bit mono at ChimeRate; nil for an unknown chime.
func ChimePCM(c Chime) []int16 {
	switch c {
	case ChimeCall:
		// High, then low as the first fades: the cabin call.
		return bells(1.6, bell{at: 0, hz: 932, gain: 0.32, decay: 0.55}, bell{at: 0.42, hz: 740, gain: 0.32, decay: 0.7})
	case ChimePA:
		return bells(1.1, bell{at: 0, hz: 1046, gain: 0.22, decay: 0.45})
	case ChimeSeatbelt:
		return bells(1.4, bell{at: 0, hz: 587, gain: 0.3, decay: 0.6})
	}
	return nil
}

// bell is one struck tone: its start (s), pitch, peak and decay time
// constant (s).
type bell struct{ at, hz, gain, decay float64 }

// bells mixes struck tones over seconds: a fundamental with soft partials,
// a 5 ms attack and an exponential decay, as a chime sounds.
func bells(seconds float64, tones ...bell) []int16 {
	n := int(seconds * ChimeRate)
	out := make([]int16, n)
	partials := []struct{ mul, gain float64 }{{1, 1}, {2.0, 0.25}, {3.0, 0.08}}
	for i := range out {
		t := float64(i) / ChimeRate
		v := 0.0
		for _, b := range tones {
			dt := t - b.at
			if dt < 0 {
				continue
			}
			env := math.Exp(-dt/b.decay) * math.Min(1, dt/0.005)
			for _, p := range partials {
				v += b.gain * p.gain * env * math.Sin(2*math.Pi*b.hz*p.mul*dt)
			}
		}
		out[i] = int16(math.Max(-1, math.Min(1, v)) * 32767)
	}
	return out
}
