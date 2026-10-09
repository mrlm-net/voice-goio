package speaker

import (
	"sync"
	"time"
)

// The floor (Options.OneAtATime): one utterance at a time across the radio,
// the intercom and the PA, each waiting for the one playing to end, never
// cutting it. Among those waiting the radio goes first, then the intercom,
// then the PA, then the ATIS broadcast. After a chime its lane keeps the
// floor a moment (the pickup), so the PA or the answer follows its chime.

// Floor priorities: lower goes first.
const (
	prioRadio = iota
	prioIntercom
	prioPA
	prioATIS
	prioCount
)

type floor struct {
	mu        sync.Mutex
	busy      bool
	owner     int
	waiting   [prioCount]int
	held      int // the lane that keeps the floor after a chime (-1 none)
	heldUntil time.Time
	wake      chan struct{} // closed when the floor may have freed
}

func newFloor() *floor {
	return &floor{held: -1, wake: make(chan struct{})}
}

// take waits for the floor at priority prio; false when done closes first.
func (f *floor) take(done <-chan struct{}, prio int) bool {
	f.mu.Lock()
	f.waiting[prio]++
	defer func() {
		f.mu.Lock()
		f.waiting[prio]--
		f.mu.Unlock()
	}()
	for {
		now := time.Now()
		held := f.held >= 0 && f.held != prio && now.Before(f.heldUntil)
		first := true
		for p := 0; p < prio; p++ {
			if f.waiting[p] > 0 && !(f.held >= 0 && f.held != p && now.Before(f.heldUntil)) {
				first = false
			}
		}
		if !f.busy && !held && first {
			f.busy, f.owner = true, prio
			if f.held == prio || !now.Before(f.heldUntil) {
				f.held = -1
			}
			f.mu.Unlock()
			return true
		}
		wake := f.wake
		var timeout <-chan time.Time
		if held {
			timeout = time.After(time.Until(f.heldUntil))
		}
		f.mu.Unlock()
		select {
		case <-done:
			f.mu.Lock() // the deferred decrement takes it again
			f.mu.Unlock()
			return false
		case <-wake:
		case <-timeout:
		}
		f.mu.Lock()
	}
}

// give frees the floor; keep > 0 keeps it for the lane that had it that
// long (after a chime: its PA or answer next).
func (f *floor) give(keep time.Duration) {
	f.mu.Lock()
	f.busy = false
	if keep > 0 {
		f.held, f.heldUntil = f.owner, time.Now().Add(keep)
	}
	close(f.wake)
	f.wake = make(chan struct{})
	f.mu.Unlock()
}

// take waits for the floor at prio when Options.OneAtATime is set (always
// true without it); false when the speaker closes meanwhile.
func (s *Speaker) take(prio int) bool {
	if s.floor == nil {
		return true
	}
	return s.floor.take(s.done, prio)
}

// give frees the floor taken with take (keep: see floor.give).
func (s *Speaker) give(keep time.Duration) {
	if s.floor != nil {
		s.floor.give(keep)
	}
}

// lanePrio is a lane's floor priority.
func lanePrio(ch Channel) int {
	if ch == ChannelPA {
		return prioPA
	}
	return prioIntercom
}

// chimeKeep: after a chime its lane keeps the floor this long (its PA or
// the answer to the call comes within it), with OneAtATime.
const chimeKeep = 3 * time.Second
