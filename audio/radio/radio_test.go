package radio_test

import (
	"math"
	"testing"

	"github.com/mrlm-net/voice-goio/audio/radio"
)

const rate = 22050

// tone renders a sine at f Hz for d seconds at -6 dBFS.
func tone(f float64, d float64) []int16 {
	n := int(rate * d)
	out := make([]int16, n)
	for i := range out {
		out[i] = int16(0.5 * 32767 * math.Sin(2*math.Pi*f*float64(i)/rate))
	}
	return out
}

// bandEnergy measures the energy of pcm at frequency f by correlating against a
// reference sine and cosine: a one bin Goertzel, which is all we need to prove
// a filter did something.
func bandEnergy(pcm []int16, f float64, sampleRate int) float64 {
	var re, im float64
	for i, s := range pcm {
		v := float64(s) / 32768
		w := 2 * math.Pi * f * float64(i) / float64(sampleRate)
		re += v * math.Cos(w)
		im += v * math.Sin(w)
	}
	n := float64(len(pcm))
	return math.Sqrt(re*re+im*im) / n
}

// The band pass is the core of the radio sound: energy below 300 Hz and above
// 3400 Hz has to be gone, while the speech band survives.
//
// Attenuation has to be measured *relative to the speech band in the same
// signal*, not in absolute terms. The chain ends by normalising the peak to
// -6 dBFS, so a signal made only of out-of-band content gets filtered to almost
// nothing and then amplified straight back up. A mixed signal shows the truth.
func TestBandPassRejectsOutOfBand(t *testing.T) {
	set := radio.Default()
	probes := []float64{60, 150, 1000, 6000}

	// One signal containing all four tones at equal amplitude.
	in := make([]int16, int(rate*0.5))
	for i := range in {
		var v float64
		for _, f := range probes {
			v += 0.2 * math.Sin(2*math.Pi*f*float64(i)/rate)
		}
		in[i] = int16(v * 32767 / float64(len(probes)))
	}
	out := set.Apply(in, rate, "tower", rate, 1)
	body := out[len(out)/4 : len(out)*3/4]

	ref := bandEnergy(body, 1000, rate)
	if ref <= 0 {
		t.Fatal("the speech band did not survive the chain at all")
	}
	for _, f := range probes {
		if f == 1000 {
			continue
		}
		att := 20 * math.Log10(bandEnergy(body, f, rate)/ref)
		if att > -20 {
			t.Errorf("%.0f Hz is only %.1f dB below the speech band, want at least 20 dB down", f, att)
		} else {
			t.Logf("%.0f Hz attenuated %.1f dB relative to 1 kHz", f, att)
		}
	}
}

// SPEC.md 9 requires the profiles to be audibly distinct. Two profiles that
// produce the same samples would fail that by definition.
func TestProfilesAreDistinct(t *testing.T) {
	set := radio.Default()
	in := tone(800, 0.4)
	outs := map[string][]int16{}
	for _, name := range set.Names() {
		outs[name] = set.Apply(in, rate, name, rate, 42)
	}
	if len(outs) < 5 {
		t.Fatalf("expected 5 profiles, got %d", len(outs))
	}
	for a := range outs {
		for b := range outs {
			if a >= b {
				continue
			}
			if identical(outs[a], outs[b]) {
				t.Errorf("profiles %q and %q produce identical audio", a, b)
			}
		}
	}
}

func identical(a, b []int16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Determinism from the seed is what lets a recorded session be reproduced and a
// regression test compare byte for byte.
func TestSeedDeterminism(t *testing.T) {
	set := radio.Default()
	in := tone(800, 0.3)
	a := set.Apply(in, rate, "center", 48000, 99)
	b := set.Apply(in, rate, "center", 48000, 99)
	if !identical(a, b) {
		t.Error("the same seed produced different audio")
	}
	if c := set.Apply(in, rate, "center", 48000, 100); identical(a, c) {
		t.Error("different seeds produced identical audio")
	}
}

// Level discipline: every transmission arrives at the same loudness whatever
// the voice did, and none of them can clip the device.
//
// The target is RMS, not peak. Speech has a crest factor around 15 dB, so
// normalising peaks to -6 dBFS leaves the speech itself at about -20 dBFS and
// sounding far away, which is the exact complaint this chain exists to avoid.
func TestOutputLevel(t *testing.T) {
	set := radio.Default()
	for _, name := range set.Names() {
		p, _ := set.Profile(name)
		for _, amp := range []float64{0.02, 1.0} {
			in := tone(900, 0.5)
			for i := range in {
				in[i] = int16(float64(in[i]) * amp)
			}
			out := set.Apply(in, rate, name, 48000, 5)

			if rms := radio.RMSdBFS(out); math.Abs(rms-p.TargetRMSDBFS) > 2 {
				t.Errorf("%s at amplitude %.2f: %.1f dBFS rms, want about %.1f",
					name, amp, rms, p.TargetRMSDBFS)
			}
			peak := 0
			for _, s := range out {
				if v := int(s); v > peak {
					peak = v
				} else if -v > peak {
					peak = -v
				}
			}
			if db := 20 * math.Log10(float64(peak)/32768); db > p.CeilingDBFS+0.5 {
				t.Errorf("%s at amplitude %.2f: peak %.1f dBFS is above the %.1f ceiling",
					name, amp, db, p.CeilingDBFS)
			}
		}
	}
}

// Compression is what makes speech sound close: the gap between the loudest
// and the quietest part of a transmission has to shrink.
func TestCompressionReducesDynamicRange(t *testing.T) {
	set := radio.Default()

	// A 900 Hz tone that steps from loud to very quiet, like a stressed vowel
	// followed by an unstressed consonant.
	n := int(rate * 0.6)
	in := make([]int16, n)
	for i := range in {
		amp := 0.6
		if i > n/2 {
			amp = 0.06 // 20 dB quieter
		}
		in[i] = int16(amp * 32767 * math.Sin(2*math.Pi*900*float64(i)/rate))
	}
	out := set.Apply(in, rate, "tower", rate, 3)
	body := out[len(out)/8 : len(out)*7/8]

	loud := rms(body[:len(body)/2])
	quiet := rms(body[len(body)/2:])
	gap := 20 * math.Log10(loud/quiet)
	if gap > 14 {
		t.Errorf("dynamic range after compression is %.1f dB, want it well under the 20 dB input", gap)
	}
	t.Logf("20 dB input range compressed to %.1f dB", gap)
}

func rms(pcm []int16) float64 {
	var sum float64
	for _, s := range pcm {
		v := float64(s) / 32768
		sum += v * v
	}
	return math.Sqrt(sum/float64(len(pcm))) + 1e-12
}

// Resampling to the device rate is the last stage, so the sample count has to
// scale with the rate.
func TestResampleToDeviceRate(t *testing.T) {
	set := radio.Default()
	in := tone(800, 1.0)
	at22 := set.Apply(in, rate, "atis", rate, 1)
	at48 := set.Apply(in, rate, "atis", 48000, 1)
	ratio := float64(len(at48)) / float64(len(at22))
	want := 48000.0 / rate
	if math.Abs(ratio-want) > 0.01 {
		t.Errorf("resample ratio %.3f, want %.3f", ratio, want)
	}
}

// An unknown profile must degrade to a plausible radio rather than to silence:
// a typo in a config file should not mute the controller.
func TestUnknownProfileFallsBack(t *testing.T) {
	set := radio.Default()
	p, ok := set.Profile("does-not-exist")
	if ok {
		t.Error("Profile reported an exact match for a name that is not in the set")
	}
	if p.Name != "tower" {
		t.Errorf("fallback profile is %q, want tower", p.Name)
	}
}

func TestEmptyInput(t *testing.T) {
	if out := radio.Default().Apply(nil, rate, "tower", 48000, 1); len(out) != 0 {
		t.Errorf("empty input produced %d samples", len(out))
	}
}
