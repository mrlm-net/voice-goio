package speaker

import (
	"sync"
	"time"

	voicegoio "github.com/mrlm-net/voice-goio"
)

// Hooks follow one utterance (Utterance.Hooks): a cabin crew panel showing
// the PA being made, an app waiting for a line to end before the next step.
//
// Each is called at most once, on its own goroutine: never under the
// speaker's locks (a hook may call the speaker) and never holding the
// audio back (a slow hook delays nothing). OnDone runs after OnStart has
// returned when OnStart was called. Either may be nil.
type Hooks struct {
	// OnStart: it starts playing, its audio given to the player (after its
	// wait for the gap, its voice and, with OneAtATime, the floor).
	OnStart func()
	// OnDone: it is done, whether it played or not: played to its end
	// (a call chime with its pickup pause), stopped (StopChannel), or
	// dropped (not queued: a full queue, the voice unavailable, the
	// speaker closed; stale; off the frequency followed or a change of
	// frequency on the radio; a synthesis error; Close). Called exactly
	// once for every utterance given with Hooks.
	OnDone func()
}

// StopFade is how long StopChannel fades out what is playing.
const StopFade = 200 * time.Millisecond

// StopChannel stops the intercom or the cabin PA (ch): what it is saying
// fades out over StopFade (after what was already sent to the device,
// 0.1 to 0.2 s on Windows), the line being synthesised or waiting its turn is dropped and so
// is everything queued on it, each with its OnDone. What is said on it
// afterwards plays as usual. The radio and its ATIS are never touched:
// false for ChannelRadio or an unknown channel, true otherwise (also when
// nothing was playing).
func (s *Speaker) StopChannel(ch Channel) bool {
	var l *lane
	switch ch {
	case ChannelIntercom:
		l = s.ic
	case ChannelPA:
		l = s.pa
	default:
		return false
	}
	s.mu.Lock()
	l.gen++
	close(l.stop)
	l.stop = make(chan struct{})
	if st, ok := l.player.(stopper); ok {
		st.Stop(StopFade)
	}
	// The lane and the voice are free once the fade has ended.
	now := time.Now()
	free := now.Add(StopFade)
	if l.mouth != "" && s.mouths[l.mouth].Equal(l.mouthEnd) && l.mouthEnd.After(free) {
		s.mouths[l.mouth] = free
	}
	l.mouth = ""
	if l.lastEnd.After(free) {
		l.lastEnd = free
	}
	if l.chimeEnd.After(now) {
		l.chimeEnd = now
	}
	drain(l.queue)
	s.mu.Unlock()
	return true
}

// stopper is a player that can fade out what it plays and drop its queue
// (audio.Player.Stop).
type stopper interface {
	Stop(fade time.Duration)
}

// track follows one queued utterance: its Hooks, each called at most once
// on its own goroutine (OnDone after OnStart has returned), and on the
// intercom or the PA the lane's stop count when it was queued.
type track struct {
	h   *Hooks
	gen uint64

	mu       sync.Mutex
	returned chan struct{} // closed when OnStart has returned; nil not called
	ended    bool
}

func newTrack(h *Hooks, gen uint64) *track {
	return &track{h: h, gen: gen}
}

// start calls OnStart, once and never after end.
func (t *track) start() {
	if t == nil || t.h == nil {
		return
	}
	t.mu.Lock()
	if t.returned != nil || t.ended {
		t.mu.Unlock()
		return
	}
	ret := make(chan struct{})
	t.returned = ret
	t.mu.Unlock()
	f := t.h.OnStart
	go func() {
		defer close(ret)
		if f != nil {
			f()
		}
	}()
}

// end calls OnDone, once, after OnStart has returned.
func (t *track) end() {
	if t == nil || t.h == nil {
		return
	}
	t.mu.Lock()
	if t.ended {
		t.mu.Unlock()
		return
	}
	t.ended = true
	ret := t.returned
	t.mu.Unlock()
	f := t.h.OnDone
	if f == nil {
		return
	}
	go func() {
		if ret != nil {
			<-ret
		}
		f()
	}()
}

// stale: l was stopped since it was queued; s.mu held.
func (t *track) stale(l *lane) bool {
	return t != nil && t.gen != l.gen
}

// playOnLane gives out to p, unless l was stopped since u was queued, and
// calls OnStart; l is busy for said from now. Under s.mu, so a stop either
// drops the line or fades it, never misses it.
func (s *Speaker) playOnLane(l *lane, u Utterance, p player, t voicegoio.Transmission, out []int16, rate int, said time.Duration) bool {
	s.mu.Lock()
	if u.tr.stale(l) {
		s.mu.Unlock()
		return false
	}
	if err := p.Play(t, out, rate); err != nil {
		s.mu.Unlock()
		return false
	}
	l.lastEnd = time.Now().Add(said)
	s.mu.Unlock()
	u.tr.start()
	return true
}

// drain empties q, each utterance's OnDone called.
func drain(q chan item) {
	for {
		select {
		case it := <-q:
			it.u.tr.end()
		default:
			return
		}
	}
}

// closed reports whether c is closed (nil never is).
func closed(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// sleepOr waits d unless the speaker is closed or stop closes meanwhile,
// and reports whether neither did.
func (s *Speaker) sleepOr(d time.Duration, stop <-chan struct{}) bool {
	if d <= 0 {
		return !closed(s.done) && !closed(stop)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-s.done:
		return false
	case <-stop:
		return false
	case <-t.C:
		return true
	}
}

// takeOr is take, given up when stop closes first.
func (s *Speaker) takeOr(prio int, stop <-chan struct{}) bool {
	if s.floor == nil {
		return !closed(stop)
	}
	return s.floor.takeOr(s.done, stop, prio)
}
