//go:build !darwin

package say

import (
	"context"

	voicegoio "github.com/mrlm-net/voice-goio"
)

// TTS is the non macOS stub. The constructor fails, so a caller that wires this
// backend by mistake finds out at start up rather than on the first
// transmission.
type TTS struct{}

// New always fails away from macOS.
func New(Options) (*TTS, error) { return nil, voicegoio.ErrNotImplemented }

func (t *TTS) Synthesize(context.Context, voicegoio.VoiceProfile, string) ([]int16, error) {
	return nil, voicegoio.ErrNotImplemented
}

func (t *TTS) SampleRate(voicegoio.VoiceProfile) int { return 22050 }

func (t *TTS) Close() error { return nil }

var _ voicegoio.TTS = (*TTS)(nil)
