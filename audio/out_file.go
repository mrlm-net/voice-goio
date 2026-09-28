package audio

// The WAV file sink: used as the only backend on platforms with no audio API
// of their own, and available on every platform for headless runs that want
// something to listen to afterwards.
//
// There is deliberately no Linux audio backend. Playing audio on Linux means
// ALSA or PulseAudio, and both mean cgo, which SPEC.md rules out. The runtime
// target is Windows.

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/internal/wav"
)

// OutDirEnv names the environment variable that selects the output directory
// for the WAV sink.
const OutDirEnv = "VOICEGOIO_OUT_DIR"

type fileSink struct {
	mu     sync.Mutex
	dir    string
	rate   int
	seq    int
	opened bool
}

func (s *fileSink) selectable() bool { return false }

func (s *fileSink) dirName() string {
	if s.dir != "" {
		return s.dir
	}
	if d := os.Getenv(OutDirEnv); d != "" {
		return d
	}
	return "."
}

func (s *fileSink) devices() ([]voicegoio.Device, error) {
	return []voicegoio.Device{{ID: DefaultDevice, Name: "WAV files in " + s.dirName(), Default: true}}, nil
}

func (s *fileSink) open(_ string, sampleRate int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.dirName()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("audio: %s: %w", dir, err)
	}
	s.dir, s.rate, s.opened = dir, sampleRate, true
	return nil
}

func (s *fileSink) write(pcm []int16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.opened {
		return voicegoio.ErrClosed
	}
	s.seq++
	return wav.WriteFile(filepath.Join(s.dir, fmt.Sprintf("tx-%04d.wav", s.seq)), pcm, s.rate)
}

func (s *fileSink) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opened = false
	return nil
}
