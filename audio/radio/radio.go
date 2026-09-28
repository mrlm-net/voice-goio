// Package radio turns clean synthesised speech into something that sounds like
// it arrived over a VHF aviation radio.
//
// The chain is parameterised per profile (SPEC.md 4.6):
//
//	band pass -> presence EQ -> compressor -> soft clip -> loudness + limiter
//	-> noise floor -> squelch -> dropouts -> resample to the device rate
//
// Order matters, and it differs from the order listed in SPEC.md 4.6 in one
// deliberate way: levelling happens on the speech, before the noise and the
// squelch bursts, rather than at the end.
//
// SPEC.md puts a peak normalise last. That measures the wrong thing twice
// over. Peak level says little about how loud speech sounds — a crest factor
// of 15 dB means -6 dBFS peak is about -20 dBFS RMS, which is exactly the
// "distant" quality we are trying not to have — and normalising after the
// squelch bursts lets a long burst drag the speech down with it. Setting the
// speech loudness first also makes the noise floor figures mean what they say:
// -45 dBFS of noise under speech at -15 dBFS RMS is a 30 dB signal to noise
// ratio, on every profile, whatever the voice did.
package radio

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"

	"github.com/mrlm-net/voice-goio/internal/dsp"
)

//go:embed radio.json
var defaultJSON []byte

// Profile is one controller position's radio character.
type Profile struct {
	Name string `json:"name"`

	BandLowHz  float64 `json:"band_low_hz"`
	BandHighHz float64 `json:"band_high_hz"`

	PresenceHz     float64 `json:"presence_hz"`
	PresenceGainDB float64 `json:"presence_gain_db"`
	PresenceQ      float64 `json:"presence_q"`

	// Compressor. This is what makes a transmission sound close rather than
	// far away; without it the consonants sit 20 dB under the vowels.
	CompThresholdDB float64 `json:"comp_threshold_db"`
	CompRatio       float64 `json:"comp_ratio"`
	CompAttackMS    float64 `json:"comp_attack_ms"`
	CompReleaseMS   float64 `json:"comp_release_ms"`

	Drive float64 `json:"drive"`

	NoiseType   string  `json:"noise_type"` // "white" or "pink"
	NoiseDBFS   float64 `json:"noise_dbfs"`
	SquelchMS   float64 `json:"squelch_ms"`
	SquelchDBFS float64 `json:"squelch_dbfs"`
	ClickDBFS   float64 `json:"click_dbfs"`

	DropoutMax   int     `json:"dropout_max"`
	DropoutMinMS int     `json:"dropout_min_ms"`
	DropoutMaxMS int     `json:"dropout_max_ms"`
	DropoutProb  float64 `json:"dropout_prob"`

	// TargetRMSDBFS is the loudness every transmission is normalised to, and
	// CeilingDBFS is the soft limit applied afterwards.
	TargetRMSDBFS float64 `json:"target_rms_dbfs"`
	CeilingDBFS   float64 `json:"ceiling_dbfs"`
}

// Set is a named collection of profiles.
type Set struct {
	Profiles []Profile `json:"profiles"`
	byName   map[string]Profile
}

// Default returns the profiles embedded in radio.json.
func Default() *Set {
	s, err := Load(defaultJSON)
	if err != nil {
		panic("radio: embedded radio.json is invalid: " + err.Error())
	}
	return s
}

// Load parses a profile set, so a user can ship their own radio.json.
func Load(b []byte) (*Set, error) {
	var s Set
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("radio: parse profiles: %w", err)
	}
	s.byName = make(map[string]Profile, len(s.Profiles))
	for _, p := range s.Profiles {
		s.byName[p.Name] = p
	}
	if len(s.byName) == 0 {
		return nil, fmt.Errorf("radio: profile set is empty")
	}
	return &s, nil
}

// Names lists the profiles in file order.
func (s *Set) Names() []string {
	out := make([]string, 0, len(s.Profiles))
	for _, p := range s.Profiles {
		out = append(out, p.Name)
	}
	return out
}

// Profile looks a profile up by name, falling back to "tower" so an unknown
// name degrades to a plausible radio instead of to silence.
func (s *Set) Profile(name string) (Profile, bool) {
	if p, ok := s.byName[name]; ok {
		return p, true
	}
	p, ok := s.byName["tower"]
	if !ok {
		return s.Profiles[0], false
	}
	return p, false
}

// Apply runs the chain over 16 bit PCM and returns 16 bit PCM at outRate.
//
// seed makes the random parts (noise, dropouts, squelch length) reproducible,
// which is what lets the regression corpus compare byte for byte.
func (s *Set) Apply(pcm []int16, inRate int, profileName string, outRate int, seed int64) []int16 {
	p, _ := s.Profile(profileName)
	return ApplyProfile(pcm, inRate, p, outRate, seed)
}

// ApplyProfile is Apply with an explicit profile.
func ApplyProfile(pcm []int16, inRate int, p Profile, outRate int, seed int64) []int16 {
	if len(pcm) == 0 {
		return nil
	}
	rng := rand.New(rand.NewSource(seed))
	buf := dsp.FromInt16(pcm)
	fr := float64(inRate)

	// 1. Band pass: two cascaded second order sections per edge (SPEC 4.6).
	for i := range 2 {
		q := dsp.Butterworth(4, i)
		dsp.HighPass(fr, p.BandLowHz, q).Process(buf)
		dsp.LowPass(fr, p.BandHighHz, q).Process(buf)
	}

	// 2. Presence bell: lift the 2-4 kHz band where /s/, /t/ and /f/ are told
	// apart. A band pass alone leaves speech muffled as well as thin.
	if p.PresenceGainDB != 0 {
		dsp.Peaking(fr, p.PresenceHz, p.PresenceQ, p.PresenceGainDB).Process(buf)
	}

	// 3. Compress, then saturate what is left.
	dsp.Compress(buf, fr, p.CompThresholdDB, p.CompRatio, p.CompAttackMS, p.CompReleaseMS, 0)
	dsp.SoftClip(buf, p.Drive)

	// 4. Set the loudness of the speech itself, before anything is added to it.
	dsp.NormaliseRMS(buf, p.TargetRMSDBFS)
	dsp.SoftLimit(buf, p.CeilingDBFS)

	// 5. Noise floor under the speech, at a level that now means something.
	addNoise(buf, p, rng)

	// 6/7. Squelch tails and dropouts.
	buf = withSquelch(buf, inRate, p, rng)
	applyDropouts(buf, inRate, p, rng)

	// 8. Rate last, so every filter ran at the model's native rate.
	return dsp.ToInt16(dsp.Resample(buf, inRate, outRate))
}

func addNoise(buf []float32, p Profile, rng *rand.Rand) {
	amp := dsp.DBToLin(p.NoiseDBFS)
	if amp <= 0 {
		return
	}
	var b0, b1, b2 float64 // Paul Kellett's pink filter state
	for i := range buf {
		w := rng.Float64()*2 - 1
		n := w
		if p.NoiseType == "pink" {
			b0 = 0.99765*b0 + w*0.0990460
			b1 = 0.96300*b1 + w*0.2965164
			b2 = 0.57000*b2 + w*1.0526913
			n = (b0 + b1 + b2 + w*0.1848) / 3
		}
		buf[i] += float32(n * amp)
	}
}

// withSquelch prepends and appends the carrier noise burst and the click that
// bracket every real transmission. The burst is what makes a stream of
// transmissions feel like a radio rather than a podcast.
func withSquelch(buf []float32, rate int, p Profile, rng *rand.Rand) []float32 {
	if p.SquelchMS <= 0 {
		return buf
	}
	// 60-100 ms, scaled around the configured nominal length.
	n := int(float64(rate) * (p.SquelchMS * (0.75 + 0.5*rng.Float64())) / 1000)
	if n <= 0 {
		return buf
	}
	amp := dsp.DBToLin(p.SquelchDBFS)
	click := dsp.DBToLin(p.ClickDBFS)

	burst := func() []float32 {
		b := make([]float32, n)
		for i := range b {
			b[i] = float32((rng.Float64()*2 - 1) * amp)
		}
		dsp.Fade(b, n/3)
		// The click is a two sample transient at the keying edge.
		b[0] = float32(click)
		b[1] = float32(-click * 0.6)
		return b
	}
	head, tail := burst(), burst()
	// The closing click belongs at the end of the tail.
	tail[0], tail[1] = 0, 0
	tail[len(tail)-1] = float32(-click)
	tail[len(tail)-2] = float32(click * 0.6)

	out := make([]float32, 0, len(head)+len(buf)+len(tail))
	out = append(out, head...)
	out = append(out, buf...)
	return append(out, tail...)
}

// applyDropouts punches short gaps into the signal, the artefact of a distant
// or blocked transmitter. Enabled for en route profiles only.
func applyDropouts(buf []float32, rate int, p Profile, rng *rand.Rand) {
	if p.DropoutMax <= 0 || rng.Float64() > p.DropoutProb {
		return
	}
	count := 1 + rng.Intn(p.DropoutMax)
	for range count {
		ms := p.DropoutMinMS + rng.Intn(max(1, p.DropoutMaxMS-p.DropoutMinMS+1))
		n := ms * rate / 1000
		if n <= 0 || n >= len(buf) {
			continue
		}
		start := rng.Intn(len(buf) - n)
		seg := buf[start : start+n]
		for i := range seg {
			seg[i] = 0
		}
		// Fade the edges of the neighbouring audio so the gap does not click.
		fadeEdge(buf, start, n, rate)
	}
}

func fadeEdge(buf []float32, start, n, rate int) {
	e := max(1, rate/1000) // 1 ms
	for i := range e {
		if start-e+i >= 0 {
			buf[start-e+i] *= float32(e-i) / float32(e)
		}
		if start+n+i < len(buf) {
			buf[start+n+i] *= float32(i) / float32(e)
		}
	}
}

// PeakDBFS reports the highest absolute sample in dBFS. Read together with
// RMSdBFS it shows how much work the limiter is doing: the gap between them is
// the crest factor left after compression, and a gap that collapses towards
// zero means the chain is squashing rather than levelling.
func PeakDBFS(pcm []int16) float64 {
	var p float64
	for _, s := range pcm {
		if v := math.Abs(float64(s) / 32768); v > p {
			p = v
		}
	}
	if p == 0 {
		return math.Inf(-1)
	}
	return 20 * math.Log10(p)
}

// RMSdBFS reports the level of a signal, used by tests and the audit runner to
// assert that a profile is audible and not clipped.
func RMSdBFS(pcm []int16) float64 {
	if len(pcm) == 0 {
		return math.Inf(-1)
	}
	var sum float64
	for _, s := range pcm {
		v := float64(s) / 32768
		sum += v * v
	}
	return 20 * math.Log10(math.Sqrt(sum/float64(len(pcm))))
}
