// Package fake is a deterministic TTS backend that needs nothing installed.
//
// It does not produce intelligible speech: it produces a voiced buzz with one
// burst per word, pitched by speaker id. That is enough to exercise everything
// downstream of synthesis (the radio chain, the queueing player, the playback
// events, cmd/voicecheck) on a machine with no piper binary and no voice
// models, which is exactly the situation in CI.
//
// Being deterministic is the point: the same profile and text always give the
// same samples, so a regression test can compare byte for byte.
package fake

import (
	"context"
	"hash/fnv"
	"math"
	"strings"

	voicegoio "github.com/mrlm-net/voice-goio"
)

// Options configures the generator.
type Options struct {
	// SampleRate defaults to 22050, matching piper's "medium" voices.
	SampleRate int
	// WordsPerMinute defaults to 180.
	WordsPerMinute int
}

// TTS implements voicegoio.TTS without any external process.
type TTS struct {
	rate int
	wpm  int
}

// New returns a generator. It never fails, which is why it is the last resort
// backend in tts.Open.
func New(opt Options) *TTS {
	if opt.SampleRate <= 0 {
		opt.SampleRate = 22050
	}
	if opt.WordsPerMinute <= 0 {
		opt.WordsPerMinute = 180
	}
	return &TTS{rate: opt.SampleRate, wpm: opt.WordsPerMinute}
}

// SampleRate is fixed at construction.
func (t *TTS) SampleRate(voicegoio.VoiceProfile) int { return t.rate }

// Synthesize renders one buzz per word.
func (t *TTS) Synthesize(_ context.Context, p voicegoio.VoiceProfile, text string) ([]int16, error) {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil, nil
	}
	wpm := float64(t.wpm)
	if p.LengthScale > 0 {
		wpm /= float64(p.LengthScale)
	}
	perWord := float64(t.rate) * 60 / wpm

	// Pitch is derived from the model name and speaker so two controllers on
	// the same frequency are distinguishable by ear in a regression run.
	f0 := 95 + float64(hashOf(p.Model)%60) + float64(p.SpeakerID%12)*3

	var out []int16
	for _, w := range words {
		n := int(perWord * wordWeight(w))
		gap := int(perWord * 0.12)
		for i := range n {
			// Three harmonics with a decaying spectrum approximate a voiced
			// sound closely enough for the band pass to have something to do.
			ph := 2 * math.Pi * f0 * float64(i) / float64(t.rate)
			v := math.Sin(ph) + 0.5*math.Sin(2*ph) + 0.25*math.Sin(3*ph)
			out = append(out, int16(v*envelope(i, n)*9000))
		}
		out = append(out, make([]int16, gap)...)
	}
	return out, nil
}

// wordWeight makes longer words take longer, so the rhythm of a clearance is
// roughly right.
func wordWeight(w string) float64 {
	return min(max(float64(len(w))/5, 0.45), 2.0)
}

// envelope is a raised cosine attack and release; a rectangular burst would
// click and the band pass would ring.
func envelope(i, n int) float64 {
	edge := max(n/8, 1)
	switch {
	case i < edge:
		return 0.5 - 0.5*math.Cos(math.Pi*float64(i)/float64(edge))
	case i > n-edge:
		return 0.5 - 0.5*math.Cos(math.Pi*float64(n-i)/float64(edge))
	}
	return 1
}

func hashOf(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

// Close is a no-op.
func (t *TTS) Close() error { return nil }

var _ voicegoio.TTS = (*TTS)(nil)
