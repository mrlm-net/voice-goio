package audio

import (
	"testing"

	voicegoio "github.com/mrlm-net/voice-goio"
)

// heldSink plays nothing until release is closed: the frequency stays busy,
// whatever the speed of the machine.
type heldSink struct {
	silentSink
	release chan struct{}
}

func (s *heldSink) write([]int16, func([]int16) ([]int16, bool)) error {
	<-s.release
	return nil
}

// A frequency that is saturated reports congestion rather than growing without
// bound: the application decides what to drop, not the library.
func TestCongestedFrequencyReportsError(t *testing.T) {
	p, err := NewPlayer(Options{Silent: true, SampleRate: 22050, QueueDepth: 2})
	if err != nil {
		t.Fatal(err)
	}
	held := &heldSink{release: make(chan struct{})}
	held.opened = true
	p.sink = held
	defer p.Close()
	defer close(held.release)

	tx := voicegoio.Transmission{Frequency: "127.450", ControllerID: "CTR", Text: "t"}
	pcm := make([]int16, 2205)
	// One in the sink at most, two queued: the fourth cannot be taken.
	var lastErr error
	for range 4 {
		if lastErr = p.Play(tx, pcm, 22050); lastErr != nil {
			break
		}
	}
	if lastErr == nil {
		t.Fatal("expected a congestion error once the queue filled")
	}
	t.Logf("congestion reported: %v", lastErr)
}
