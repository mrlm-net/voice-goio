//go:build darwin

package say

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/internal/wav"
)

// TTS renders through the macOS `say` command, one process per utterance.
//
// Unlike piper there is no warm process to keep: `say` starts in a few tens of
// milliseconds because the voices are already resident in the OS. That is also
// why this backend cannot be the shipping one: it exists only on macOS.
type TTS struct {
	opt    Options
	dir    string
	seq    atomic.Int64
	once   sync.Once
	closed atomic.Bool
}

// New checks that `say` exists and prepares a scratch directory.
func New(opt Options) (*TTS, error) {
	if _, err := exec.LookPath("say"); err != nil {
		return nil, fmt.Errorf("say: not available: %w", err)
	}
	if opt.SampleRate <= 0 {
		opt.SampleRate = 22050
	}
	dir, err := os.MkdirTemp("", "voicegoio-say-")
	if err != nil {
		return nil, err
	}
	return &TTS{opt: opt, dir: dir}, nil
}

// SampleRate is fixed by Options: `say` renders at whatever rate we ask for.
func (t *TTS) SampleRate(voicegoio.VoiceProfile) int { return t.opt.SampleRate }

// Synthesize renders text to a temporary WAV and reads it back as PCM.
func (t *TTS) Synthesize(ctx context.Context, p voicegoio.VoiceProfile, text string) ([]int16, error) {
	if t.closed.Load() {
		return nil, voicegoio.ErrClosed
	}
	if text == "" {
		return nil, nil
	}
	path := filepath.Join(t.dir, fmt.Sprintf("u-%d.wav", t.seq.Add(1)))
	defer os.Remove(path)

	args := []string{
		"-v", voiceFor(t.opt, p),
		"-r", strconv.Itoa(rateFor(t.opt, p)),
		"-o", path,
		"--data-format=LEI16@" + strconv.Itoa(t.opt.SampleRate),
		text,
	}
	cmd := exec.CommandContext(ctx, "say", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("say: %w: %s", err, out)
	}
	pcm, _, err := wav.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("say: read rendered audio: %w", err)
	}
	return pcm, nil
}

// Close removes the scratch directory.
func (t *TTS) Close() error {
	if t.closed.Swap(true) {
		return nil
	}
	return os.RemoveAll(t.dir)
}

var _ voicegoio.TTS = (*TTS)(nil)
