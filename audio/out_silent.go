package audio

// The silent sink: accepts audio and plays nothing.
//
// It exists for rendering. A run that is only producing a recording should not
// also be shouting through the speakers for several minutes, and a headless
// or CI run has no device to shout through in the first place. Recording is
// done by the Player, not by the sink, so a silent run still records
// everything.

import (
	"sync"

	voicegoio "github.com/mrlm-net/voice-goio"
)

type silentSink struct {
	mu     sync.Mutex
	opened bool
}

func (s *silentSink) selectable() bool { return false }

func (s *silentSink) devices() ([]voicegoio.Device, error) {
	return []voicegoio.Device{{ID: DefaultDevice, Name: "Silent (no playback)", Default: true}}, nil
}

func (s *silentSink) open(string, int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opened = true
	return nil
}

func (s *silentSink) write([]int16, func() float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.opened {
		return voicegoio.ErrClosed
	}
	return nil
}

func (s *silentSink) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opened = false
	return nil
}
