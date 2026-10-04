package speaker

import "math"

// PAChain is pcm (16-bit mono at rate) as the cabin's speakers give it: band
// limited (about 300 Hz to 4 kHz, small ceiling speakers) with a little of
// the cabin's room (two early reflections). Lighter than the radio chain:
// no noise, no squelch.
func PAChain(pcm []int16, rate int) []int16 {
	if len(pcm) == 0 || rate <= 0 {
		return pcm
	}
	hp := newBiquad(highPass, 300, rate)
	lp := newBiquad(lowPass, 4000, rate)
	x := make([]float64, len(pcm))
	for i, v := range pcm {
		x[i] = lp.step(hp.step(float64(v) / 32768))
	}
	// Two reflections off the cabin's walls and ceiling.
	d1, d2 := int(0.011*float64(rate)), int(0.023*float64(rate))
	out := make([]int16, len(pcm))
	for i := range x {
		v := x[i]
		if i >= d1 {
			v += 0.28 * x[i-d1]
		}
		if i >= d2 {
			v += 0.16 * x[i-d2]
		}
		out[i] = int16(math.Max(-1, math.Min(1, v*0.85)) * 32767)
	}
	return out
}

type filterKind int

const (
	lowPass filterKind = iota
	highPass
)

// biquad is an RBJ cookbook second-order filter (Q 0.707).
type biquad struct {
	b0, b1, b2, a1, a2 float64
	x1, x2, y1, y2     float64
}

func newBiquad(k filterKind, hz float64, rate int) *biquad {
	w := 2 * math.Pi * hz / float64(rate)
	alpha := math.Sin(w) / (2 * 0.7071) // Q = 0.707
	cw := math.Cos(w)
	var b0, b1, b2 float64
	if k == lowPass {
		b0, b1, b2 = (1-cw)/2, 1-cw, (1-cw)/2
	} else {
		b0, b1, b2 = (1+cw)/2, -(1 + cw), (1+cw)/2
	}
	a0, a1, a2 := 1+alpha, -2*cw, 1-alpha
	return &biquad{b0: b0 / a0, b1: b1 / a0, b2: b2 / a0, a1: a1 / a0, a2: a2 / a0}
}

func (f *biquad) step(x float64) float64 {
	y := f.b0*x + f.b1*f.x1 + f.b2*f.x2 - f.a1*f.y1 - f.a2*f.y2
	f.x2, f.x1, f.y2, f.y1 = f.x1, x, f.y1, y
	return y
}
