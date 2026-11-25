package audio

import (
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	voicegoio "github.com/mrlm-net/voice-goio"
)

// gateSink plays one part of 100 samples per tick sent on gate and keeps
// what each write sent to the device; shaped counts the parts shaped.
type gateSink struct {
	gate   chan struct{}
	shaped atomic.Int64
	mu     sync.Mutex
	got    [][]int16
}

func (s *gateSink) devices() ([]voicegoio.Device, error) { return nil, nil }
func (s *gateSink) open(string, int) error               { return nil }
func (s *gateSink) close() error                         { return nil }
func (s *gateSink) selectable() bool                     { return false }

func (s *gateSink) write(pcm []int16, shape func([]int16) ([]int16, bool)) error {
	var out []int16
	for off := 0; off < len(pcm); off += 100 {
		<-s.gate
		part, last := shape(pcm[off:min(off+100, len(pcm))])
		s.shaped.Add(1)
		out = append(out, part...)
		if last {
			break
		}
	}
	s.mu.Lock()
	s.got = append(s.got, out)
	s.mu.Unlock()
	return nil
}

func (s *gateSink) writes() [][]int16 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]int16(nil), s.got...)
}

// gatedPlayer is a Player at 1 kHz on a gateSink.
func gatedPlayer(t *testing.T) (*Player, *gateSink) {
	t.Helper()
	s := &gateSink{gate: make(chan struct{})}
	p := &Player{sink: s, rate: 1000, depth: 16, queues: map[string]chan item{}, events: make(chan voicegoio.PlaybackEvent, 128), done: make(chan struct{})}
	p.gain.Store(math.Float64bits(1))
	go func() {
		for range p.events {
		}
	}()
	t.Cleanup(func() { p.Close() })
	return p, s
}

// tick plays one part and waits until it is shaped, so a Stop after it
// lands on the next part, not on this one.
func (s *gateSink) tick(t *testing.T) {
	t.Helper()
	n := s.shaped.Load()
	s.gate <- struct{}{}
	for end := time.Now().Add(3 * time.Second); s.shaped.Load() == n; time.Sleep(time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal("part never shaped")
		}
	}
}

func level(n int, v int16) []int16 {
	out := make([]int16, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func waitWrites(t *testing.T, s *gateSink, n int) [][]int16 {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(2 * time.Millisecond) {
		if w := s.writes(); len(w) >= n {
			return w
		}
	}
	t.Fatalf("%d writes, want %d", len(s.writes()), n)
	return nil
}

// Stop fades what is playing to silence over its fade, as it goes out,
// drops what was queued before it, and plays what is queued after it in
// full.
func TestStopFadesAndDrops(t *testing.T) {
	p, s := gatedPlayer(t)
	tx := voicegoio.Transmission{Frequency: "pa"}
	if err := p.Play(tx, level(1000, 10000), 1000); err != nil {
		t.Fatal(err)
	}
	if err := p.Play(tx, level(500, 10000), 1000); err != nil { // queued: dropped
		t.Fatal(err)
	}
	for range 3 {
		s.tick(t) // 300 samples at full
	}
	p.Stop(50 * time.Millisecond) // 50 samples at 1 kHz
	s.gate <- struct{}{}
	if err := p.Play(tx, level(200, 10000), 1000); err != nil {
		t.Fatal(err)
	}
	s.gate <- struct{}{}
	s.gate <- struct{}{}
	w := waitWrites(t, s, 2)
	first := w[0]
	if len(first) != 350 {
		t.Fatalf("stopped line sent %d samples, want 300 + a 50-sample fade", len(first))
	}
	for i := 0; i < 300; i++ {
		if first[i] != 10000 {
			t.Fatalf("sample %d before the stop at %d", i, first[i])
		}
	}
	fade := first[300:]
	if fade[0] != 10000 || fade[49] != 200 {
		t.Errorf("fade from %d to %d, want 10000 to 200", fade[0], fade[49])
	}
	for i := 1; i < len(fade); i++ {
		if fade[i] >= fade[i-1] {
			t.Fatalf("fade not falling at %d: %d after %d", i, fade[i], fade[i-1])
		}
	}
	if len(w) != 2 || len(w[1]) != 200 || w[1][0] != 10000 || w[1][199] != 10000 {
		t.Errorf("after the stop: %d writes, the last %d samples", len(w), len(w[len(w)-1]))
	}
}

// A zero fade cuts at once; the gain still applies before a stop.
func TestStopCutsAndKeepsGain(t *testing.T) {
	p, s := gatedPlayer(t)
	p.SetGain(0.5)
	tx := voicegoio.Transmission{Frequency: "intercom"}
	if err := p.Play(tx, level(1000, 10000), 1000); err != nil {
		t.Fatal(err)
	}
	s.tick(t)
	p.Stop(0)
	s.gate <- struct{}{}
	w := waitWrites(t, s, 1)
	if len(w[0]) != 100 || w[0][0] != 5000 {
		t.Errorf("sent %d samples starting %d, want 100 at 5000", len(w[0]), w[0][0])
	}
}

// Without a Stop the shaper is the gain alone.
func TestShaperWithoutStop(t *testing.T) {
	p, _ := gatedPlayer(t)
	sh := p.shaper(p.cuts.Load())
	in := level(10, 1000)
	out, last := sh(in)
	if last || &out[0] != &in[0] {
		t.Errorf("full volume shaped: last %v", last)
	}
	p.SetGain(0.25)
	if out, _ := sh(in); out[0] != 250 {
		t.Errorf("gain: %d", out[0])
	}
}
