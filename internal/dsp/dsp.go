// Package dsp holds the signal processing primitives the radio chain is built
// from: RBJ biquads, sample rate conversion and level helpers.
//
// Everything works on []float32 in the range [-1, 1]. Conversion to and from
// int16 happens once, at the edges of the chain.
package dsp

import "math"

// Biquad is a direct form I biquad with normalised coefficients.
type Biquad struct {
	b0, b1, b2, a1, a2 float64
	x1, x2, y1, y2     float64
}

// Reset clears the filter state so one Biquad value can be reused per signal.
func (f *Biquad) Reset() { f.x1, f.x2, f.y1, f.y2 = 0, 0, 0, 0 }

// Process filters in place.
func (f *Biquad) Process(buf []float32) {
	for i, v := range buf {
		x := float64(v)
		y := f.b0*x + f.b1*f.x1 + f.b2*f.x2 - f.a1*f.y1 - f.a2*f.y2
		f.x2, f.x1 = f.x1, x
		f.y2, f.y1 = f.y1, y
		buf[i] = float32(y)
	}
}

// LowPass and HighPass are RBJ cookbook sections; Q = 1/sqrt(2) gives a
// second order Butterworth response, and cascading two of them gives the fourth
// order skirt that makes speech sound like it came out of a radio.
func LowPass(sampleRate, freq, q float64) *Biquad {
	w0, alpha := omega(sampleRate, freq, q)
	cos := math.Cos(w0)
	b0 := (1 - cos) / 2
	b1 := 1 - cos
	b2 := b0
	a0 := 1 + alpha
	return norm(b0, b1, b2, a0, -2*cos, 1-alpha)
}

func HighPass(sampleRate, freq, q float64) *Biquad {
	w0, alpha := omega(sampleRate, freq, q)
	cos := math.Cos(w0)
	b0 := (1 + cos) / 2
	b1 := -(1 + cos)
	b2 := b0
	a0 := 1 + alpha
	return norm(b0, b1, b2, a0, -2*cos, 1-alpha)
}

// Peaking is the presence bell that lifts consonants out of the band pass.
func Peaking(sampleRate, freq, q, gainDB float64) *Biquad {
	a := math.Pow(10, gainDB/40)
	w0, alpha := omega(sampleRate, freq, q)
	cos := math.Cos(w0)
	return norm(1+alpha*a, -2*cos, 1-alpha*a, 1+alpha/a, -2*cos, 1-alpha/a)
}

func omega(sampleRate, freq, q float64) (w0, alpha float64) {
	if freq >= sampleRate/2 {
		freq = sampleRate/2 - 1
	}
	w0 = 2 * math.Pi * freq / sampleRate
	return w0, math.Sin(w0) / (2 * q)
}

func norm(b0, b1, b2, a0, a1, a2 float64) *Biquad {
	return &Biquad{b0: b0 / a0, b1: b1 / a0, b2: b2 / a0, a1: a1 / a0, a2: a2 / a0}
}

// Butterworth is Q for the k-th section of an n-th order Butterworth cascade.
func Butterworth(order, section int) float64 {
	return 1 / (2 * math.Sin(math.Pi*(2*float64(section)+1)/(2*float64(order))))
}

// SoftClip applies tanh saturation, the cheap stand-in for the compression and
// overmodulation of a real transmitter. Output is renormalised by tanh(drive)
// so the drive control changes character, not level.
func SoftClip(buf []float32, drive float64) {
	if drive <= 0 {
		return
	}
	k := math.Tanh(drive)
	for i, v := range buf {
		buf[i] = float32(math.Tanh(float64(v)*drive) / k)
	}
}

// Peak returns the largest absolute sample.
func Peak(buf []float32) float64 {
	var p float64
	for _, v := range buf {
		if a := math.Abs(float64(v)); a > p {
			p = a
		}
	}
	return p
}

// NormalisePeak scales buf so its peak sits at targetDBFS.
func NormalisePeak(buf []float32, targetDBFS float64) {
	p := Peak(buf)
	if p == 0 {
		return
	}
	g := DBToLin(targetDBFS) / p
	for i := range buf {
		buf[i] = float32(float64(buf[i]) * g)
	}
}

// DBToLin converts dBFS to a linear amplitude.
func DBToLin(db float64) float64 { return math.Pow(10, db/20) }

// Resample converts between sample rates with linear interpolation.
//
// Linear interpolation aliases above roughly 0.4 of the Nyquist rate, which
// would matter for music. It does not matter here: the radio chain has already
// band limited the signal to 3.4 kHz, an octave below the Nyquist rate of even
// the lowest rate we resample from, so there is nothing left up there to fold.
func Resample(in []float32, from, to int) []float32 {
	if from == to || len(in) == 0 {
		return in
	}
	ratio := float64(from) / float64(to)
	n := int(float64(len(in)) / ratio)
	out := make([]float32, n)
	for i := range out {
		pos := float64(i) * ratio
		j := int(pos)
		frac := float32(pos - float64(j))
		if j+1 < len(in) {
			out[i] = in[j]*(1-frac) + in[j+1]*frac
		} else {
			out[i] = in[len(in)-1]
		}
	}
	return out
}

// FromInt16 and ToInt16 convert at the edges of the chain. ToInt16 clamps
// rather than wrapping: a wrapped sample is a loud click.
func FromInt16(pcm []int16) []float32 {
	out := make([]float32, len(pcm))
	for i, s := range pcm {
		out[i] = float32(s) / 32768
	}
	return out
}

func ToInt16(buf []float32) []int16 {
	out := make([]int16, len(buf))
	for i, v := range buf {
		s := float64(v) * 32767
		switch {
		case s > 32767:
			s = 32767
		case s < -32768:
			s = -32768
		}
		out[i] = int16(s)
	}
	return out
}

// Fade applies a linear fade in and out of n samples, used to keep dropouts and
// squelch bursts from clicking.
func Fade(buf []float32, n int) {
	if n <= 0 || len(buf) < 2*n {
		return
	}
	for i := range n {
		g := float32(i) / float32(n)
		buf[i] *= g
		buf[len(buf)-1-i] *= g
	}
}

// Compress is a feed-forward compressor with a peak envelope follower.
//
// It is the single biggest intelligibility win in the radio chain. Speech has a
// crest factor around 15 dB: the peaks of a stressed vowel are far above the
// consonants that actually carry the meaning. A real transmitter compresses
// hard, which is why radio speech sounds close and present even through a
// 3 kHz band. Without this stage, normalising for peaks leaves the body of the
// speech 20 dB down and the result sounds far away.
//
// thresholdDB and makeupDB are dBFS, ratio is n:1, attack and release are
// milliseconds.
func Compress(buf []float32, sampleRate, thresholdDB, ratio, attackMS, releaseMS, makeupDB float64) {
	if ratio <= 1 || len(buf) == 0 {
		return
	}
	thr := DBToLin(thresholdDB)
	makeup := DBToLin(makeupDB)
	att := coeff(attackMS, sampleRate)
	rel := coeff(releaseMS, sampleRate)

	// Prime the envelope from the level of the opening window rather than from
	// zero. A follower starting at zero passes the first few milliseconds at
	// unity gain while everything after it is pulled down, and the loudness
	// normalisation that follows then amplifies that overshoot into a click at
	// the start of every transmission.
	env := primeEnvelope(buf, sampleRate, attackMS)
	for i, v := range buf {
		x := math.Abs(float64(v))
		// Fast attack, slow release: clamp a transient immediately, then let
		// the gain recover so the following quiet syllable is lifted.
		if x > env {
			env = att*env + (1-att)*x
		} else {
			env = rel*env + (1-rel)*x
		}
		g := 1.0
		if env > thr && env > 0 {
			// Above the threshold, only 1/ratio of the excess survives.
			g = math.Pow(env/thr, 1/ratio-1)
		}
		buf[i] = float32(float64(v) * g * makeup)
	}
}

// primeEnvelope estimates the peak level of the first attack window, so the
// follower starts where the signal already is.
func primeEnvelope(buf []float32, sampleRate, attackMS float64) float64 {
	w := int(math.Max(attackMS, 10) * 0.001 * sampleRate)
	if w > len(buf) {
		w = len(buf)
	}
	if w == 0 {
		return 0
	}
	var sum float64
	for _, v := range buf[:w] {
		sum += float64(v) * float64(v)
	}
	// RMS to peak for a roughly sinusoidal opening.
	return math.Sqrt(sum/float64(w)) * math.Sqrt2
}

// coeff converts a time constant in milliseconds to a one pole smoothing
// coefficient.
func coeff(ms, sampleRate float64) float64 {
	if ms <= 0 {
		return 0
	}
	return math.Exp(-1 / (ms * 0.001 * sampleRate))
}

// RMSdB reports the RMS level of a buffer in dBFS.
func RMSdB(buf []float32) float64 {
	if len(buf) == 0 {
		return math.Inf(-1)
	}
	var sum float64
	for _, v := range buf {
		sum += float64(v) * float64(v)
	}
	return 20 * math.Log10(math.Sqrt(sum/float64(len(buf))))
}

// NormaliseRMS scales buf so its RMS sits at targetDBFS.
//
// Loudness rather than peak: this is what makes one controller as audible as
// the next regardless of how the model happened to render the phrase.
func NormaliseRMS(buf []float32, targetDBFS float64) {
	cur := RMSdB(buf)
	if math.IsInf(cur, -1) {
		return
	}
	g := DBToLin(targetDBFS - cur)
	for i := range buf {
		buf[i] = float32(float64(buf[i]) * g)
	}
}

// SoftLimit bends the signal so it can never exceed the ceiling.
//
// Below the knee, at half the ceiling amplitude, samples pass through exactly
// unchanged: ordinary speech is untouched. Above it the remaining headroom is
// bent through a tanh that is asymptotic to the ceiling itself, so the ceiling
// is a guarantee rather than a knee position, and an occasional peak is rounded
// off in a way that sounds like a saturating transmitter rather than like
// clipping.
func SoftLimit(buf []float32, ceilingDBFS float64) {
	c := DBToLin(ceilingDBFS)
	if c <= 0 {
		return
	}
	knee := c / 2
	span := c - knee
	for i, v := range buf {
		x := float64(v)
		a := math.Abs(x)
		if a <= knee {
			continue
		}
		buf[i] = float32(math.Copysign(knee+span*math.Tanh((a-knee)/span), x))
	}
}
