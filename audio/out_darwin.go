//go:build darwin

package audio

// macOS playback, for development only.
//
// SPEC.md 4.7 specifies afplay: write the utterance to a temporary WAV and hand
// it to the system player. That is a subprocess per transmission and gives no
// device selection, which is fine because the shipping target is Windows and
// this backend exists so the pipeline can be heard end to end on the
// development machine.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/internal/wav"
)

type darwinSink struct {
	mu     sync.Mutex
	dir    string
	rate   int
	seq    int
	opened bool
}

func newSink() (sink, error) { return &darwinSink{}, nil }

// selectable is false: SetDevice is a documented no-op here.
func (s *darwinSink) selectable() bool { return false }

func (s *darwinSink) devices() ([]voicegoio.Device, error) {
	return []voicegoio.Device{{
		ID:      DefaultDevice,
		Name:    "System default (macOS afplay, not selectable)",
		Default: true,
	}}, nil
}

func (s *darwinSink) open(_ string, sampleRate int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := os.MkdirTemp("", "voicegoio-play-")
	if err != nil {
		return fmt.Errorf("audio: temp dir: %w", err)
	}
	s.dir, s.rate, s.opened = dir, sampleRate, true
	return nil
}

func (s *darwinSink) write(pcm []int16, gain func() float64) error {
	pcm = scaled(pcm, gain())
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.opened {
		return voicegoio.ErrClosed
	}
	s.seq++
	path := filepath.Join(s.dir, fmt.Sprintf("tx-%d.wav", s.seq))
	if err := wav.WriteFile(path, pcm, s.rate); err != nil {
		return err
	}
	defer os.Remove(path)
	if out, err := exec.Command("afplay", path).CombinedOutput(); err != nil {
		return fmt.Errorf("audio: afplay: %w: %s", err, out)
	}
	return nil
}

func (s *darwinSink) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.opened {
		return nil
	}
	s.opened = false
	return os.RemoveAll(s.dir)
}
