package dsp

import (
	"math"
	"testing"
)

const rate = 22050

func sine(f, amp, seconds float64) []float32 {
	n := int(rate * seconds)
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(amp * math.Sin(2*math.Pi*f*float64(i)/rate))
	}
	return out
}

func peakDB(b []float32) float64 { return 20 * math.Log10(Peak(b)) }

// The level staging of the whole chain, stage by stage. This is the test that
// catches the two ways a radio chain sounds wrong: too quiet overall, and a
// click at the start of every transmission.
func TestStageLevels(t *testing.T) {
	buf := sine(900, 0.5, 0.5)
	stage := func(label string) (peak, rms float64) {
		p, r := peakDB(buf), RMSdB(buf)
		t.Logf("%-10s peak %6.1f  rms %6.1f", label, p, r)
		return p, r
	}
	stage("input")
	for i := range 2 {
		q := Butterworth(4, i)
		HighPass(rate, 280, q).Process(buf)
		LowPass(rate, 3600, q).Process(buf)
	}
	stage("bandpass")
	Peaking(rate, 2600, 0.9, 6).Process(buf)
	stage("presence")
	Compress(buf, rate, -26, 4, 4, 90, 0)
	cPeak, cRMS := stage("compress")

	// A steady tone must come out steady: the gap between peak and RMS is the
	// crest factor of a sine, about 3 dB. Anything more means the attack of
	// the signal escaped the compressor.
	if crest := cPeak - cRMS; crest > 5 {
		t.Errorf("crest factor after compression is %.1f dB, want about 3 for a steady tone: "+
			"the envelope follower let the attack through", crest)
	}

	SoftClip(buf, 1.3)
	stage("softclip")
	NormaliseRMS(buf, -15)
	stage("loudness")
	SoftLimit(buf, -3)
	p, r := stage("limiter")

	if math.Abs(r-(-15)) > 1 {
		t.Errorf("final rms %.1f dBFS, want -15", r)
	}
	if p > -3 {
		t.Errorf("final peak %.1f dBFS is above the -3 ceiling", p)
	}
}

// The ceiling has to be a guarantee, not a suggestion: a limiter that merely
// bends peaks towards full scale still hands clipped samples to the device.
func TestSoftLimitRespectsItsCeiling(t *testing.T) {
	for _, ceil := range []float64{-3, -6, -12} {
		buf := sine(1000, 4.0, 0.05) // grossly over scale
		SoftLimit(buf, ceil)
		if got := peakDB(buf); got > ceil+0.01 {
			t.Errorf("ceiling %.0f dBFS: peak came out at %.2f dBFS", ceil, got)
		}
	}
}

// Quiet signals must pass through the limiter essentially untouched.
func TestSoftLimitIsTransparentBelowTheCeiling(t *testing.T) {
	before := sine(1000, 0.05, 0.05) // -26 dBFS, well under a -3 ceiling
	after := append([]float32(nil), before...)
	SoftLimit(after, -3)
	for i := range before {
		if d := math.Abs(float64(before[i] - after[i])); d > 0.002 {
			t.Fatalf("sample %d changed by %.4f, want the limiter to be transparent here", i, d)
		}
	}
}

func TestCompressReducesRange(t *testing.T) {
	loud := sine(900, 0.6, 0.3)
	quiet := sine(900, 0.06, 0.3) // 20 dB down
	before := RMSdB(loud) - RMSdB(quiet)

	Compress(loud, rate, -26, 4, 4, 90, 0)
	Compress(quiet, rate, -26, 4, 4, 90, 0)
	after := RMSdB(loud) - RMSdB(quiet)

	if after >= before {
		t.Errorf("range %.1f dB before, %.1f dB after: the compressor did nothing", before, after)
	}
	t.Logf("%.1f dB range compressed to %.1f dB", before, after)
}

func TestNormaliseRMSHitsTheTarget(t *testing.T) {
	for _, target := range []float64{-15, -20, -6} {
		buf := sine(440, 0.3, 0.2)
		NormaliseRMS(buf, target)
		if got := RMSdB(buf); math.Abs(got-target) > 0.01 {
			t.Errorf("target %.0f dBFS, got %.2f", target, got)
		}
	}
}

// Resampling must preserve duration and not invent level.
func TestResample(t *testing.T) {
	in := sine(440, 0.5, 0.1)
	out := Resample(in, rate, 48000)
	want := float64(len(in)) * 48000 / rate
	if math.Abs(float64(len(out))-want) > 2 {
		t.Errorf("resampled to %d samples, want about %.0f", len(out), want)
	}
	if Peak(out) > Peak(in)*1.05 {
		t.Errorf("resampling raised the peak from %.3f to %.3f", Peak(in), Peak(out))
	}
	if same := Resample(in, rate, rate); &same[0] != &in[0] {
		t.Error("resampling to the same rate should return the input unchanged")
	}
}

func TestToInt16Clamps(t *testing.T) {
	out := ToInt16([]float32{2.0, -2.0, 0})
	if out[0] != 32767 || out[1] != -32768 || out[2] != 0 {
		t.Errorf("clamping failed: %v", out)
	}
}
