package speaker

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	voicegoio "github.com/mrlm-net/voice-goio"
)

func (p *fakePlayer) Stop(fade time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stops = append(p.stops, fade)
}

func (p *fakePlayer) stopped() []time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]time.Duration(nil), p.stops...)
}

var ryan = voicegoio.VoiceProfile{Model: "en_US-ryan-medium", SpeakerID: 0, Accent: "en-US"}

// hookLog keeps what the hooks were told, in order, with when.
type hookLog struct {
	mu sync.Mutex
	ev []string
	at map[string]time.Time
}

func newHookLog() *hookLog { return &hookLog{at: map[string]time.Time{}} }

func (h *hookLog) add(e string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ev = append(h.ev, e)
	h.at[e] = time.Now()
}

func (h *hookLog) hooks(name string) *Hooks {
	return &Hooks{OnStart: func() { h.add(name + " start") }, OnDone: func() { h.add(name + " done") }}
}

func (h *hookLog) events() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.ev...)
}

func (h *hookLog) has(e string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.at[e]
	return ok
}

func (h *hookLog) when(e string) time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.at[e]
}

// laneP is ch's player in the rig (nil before it opens).
func laneP(r *rig, ch Channel) *fakePlayer {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	p, _ := r.s.playerOf(ch).(*fakePlayer)
	return p
}

func paLine(text string, v voicegoio.VoiceProfile, h *Hooks) Utterance {
	return Utterance{PA: true, Position: "purser", Text: text, Voice: &v, Hooks: h}
}

func icLine(text string, v voicegoio.VoiceProfile, h *Hooks) Utterance {
	return Utterance{Intercom: true, Position: "copilot", Text: text, Voice: &v, Hooks: h}
}

// OnStart as a line starts playing, OnDone once it has been said, each
// once and in that order, on the intercom, the PA and the radio.
func TestHooksStartThenDone(t *testing.T) {
	r := newRig(t, fast, Options{})
	log := newHookLog()
	r.s.Set(true, "118.105")
	if !r.s.SayOnce(icLine("Flaps one", alan, log.hooks("ic"))) {
		t.Fatal("intercom refused")
	}
	if !r.s.SayOnce(paLine("Welcome aboard", ryan, log.hooks("pa"))) {
		t.Fatal("PA refused")
	}
	u := tower("CSA123, cleared to land")
	u.Hooks = log.hooks("radio")
	r.s.Hear(u)
	eventually(t, "all done", func() bool { return log.has("ic done") && log.has("pa done") && log.has("radio done") })
	for _, name := range []string{"ic", "pa", "radio"} {
		n := 0
		for _, e := range log.events() {
			if strings.HasPrefix(e, name+" ") {
				n++
			}
		}
		if n != 2 {
			t.Errorf("%s: %d calls in %v", name, n, log.events())
		}
		if !log.when(name + " start").Before(log.when(name + " done")) {
			t.Errorf("%s: done before start", name)
		}
	}
	said := samplesDuration(len(SpokenEnd("Flaps one"))+int(TailPad.Seconds()*fakeRate), fakeRate)
	if d := log.when("ic done").Sub(log.when("ic start")); d < said-20*time.Millisecond {
		t.Errorf("intercom done %v after its start, said for %v", d, said)
	}
	if p := laneP(r, ChannelIntercom); p == nil || len(p.got()) != 1 {
		t.Error("intercom not played once")
	}
}

// A line not played is done without a start: the radio off or off the
// frequency, the speaker closed.
func TestHooksDropped(t *testing.T) {
	r := newRig(t, fast, Options{})
	log := newHookLog()
	u := tower("off")
	u.Hooks = log.hooks("off")
	r.s.Hear(u)
	r.s.Set(true, "118.105")
	u = tower("other")
	u.Frequency, u.Hooks = "120.530", log.hooks("other")
	r.s.Hear(u)
	eventually(t, "dropped", func() bool { return log.has("off done") && log.has("other done") })
	r.s.Close()
	if r.s.SayPA("Closed", ryan) {
		t.Error("PA said after Close")
	}
	if r.s.SayOnce(paLine("Closed", ryan, log.hooks("closed"))) {
		t.Error("PA queued after Close")
	}
	r.s.Hear(icLine("Closed", alan, log.hooks("heard")))
	eventually(t, "closed", func() bool { return log.has("closed done") && log.has("heard done") })
	for _, e := range log.events() {
		if strings.HasSuffix(e, " start") {
			t.Errorf("started: %s", e)
		}
	}
}

// Hooks run off the speaker's locks and never hold the audio back: a hook
// that calls the speaker and blocks leaves the next line to play; its
// OnDone waits for its OnStart to return.
func TestHooksOffTheLocks(t *testing.T) {
	r := newRig(t, fast, Options{})
	log := newHookLog()
	release := make(chan struct{})
	h := &Hooks{
		OnStart: func() {
			_ = r.s.State()
			r.s.SetGain(ChannelIntercom, 1)
			r.s.SayIntercom("Second", ryan)
			<-release
			log.add("first start")
		},
		OnDone: func() { log.add("first done") },
	}
	if !r.s.SayOnce(icLine("First", alan, h)) {
		t.Fatal("refused")
	}
	eventually(t, "second played", func() bool { p := laneP(r, ChannelIntercom); return p != nil && len(p.got()) == 2 })
	time.Sleep(50 * time.Millisecond) // the first line has long been said
	if log.has("first done") {
		t.Fatal("OnDone before OnStart returned")
	}
	close(release)
	eventually(t, "first done", func() bool { return log.has("first done") })
	if ev := log.events(); len(ev) != 2 || ev[0] != "first start" {
		t.Errorf("events %v", ev)
	}
}

// StopChannel fades the PA out, drops its queue, frees the voice and the
// lane for what comes next, and leaves the intercom and the radio alone.
func TestStopChannel(t *testing.T) {
	r := newRig(t, fast, Options{})
	log := newHookLog()
	r.s.Set(true, "118.105")
	long := strings.Repeat("a", 3000) // 3 s
	r.s.SayOnce(paLine(long, alan, log.hooks("long")))
	r.s.SayOnce(paLine("next one", alan, log.hooks("next1")))
	r.s.SayOnce(paLine("next two", alan, log.hooks("next2")))
	r.s.SayOnce(icLine(strings.Repeat("b", 600), ryan, log.hooks("ic")))
	eventually(t, "long started", func() bool { return log.has("long start") && log.has("ic start") })
	if r.s.StopChannel(ChannelRadio) || r.s.StopChannel("cabin") {
		t.Error("the radio or an unknown channel stopped")
	}
	stop := time.Now()
	if !r.s.StopChannel(ChannelPA) {
		t.Fatal("PA not stopped")
	}
	eventually(t, "PA done", func() bool { return log.has("long done") && log.has("next1 done") && log.has("next2 done") })
	if d := time.Since(stop); d > time.Second {
		t.Errorf("PA done %v after the stop", d)
	}
	if log.has("next1 start") || log.has("next2 start") {
		t.Errorf("queued PA started: %v", log.events())
	}
	pa := laneP(r, ChannelPA)
	if got := pa.stopped(); len(got) != 1 || got[0] != StopFade {
		t.Errorf("PA player stops %v", got)
	}
	if n := len(pa.got()); n != 1 {
		t.Errorf("PA played %d lines", n)
	}
	if ic := laneP(r, ChannelIntercom); len(ic.stopped()) != 0 {
		t.Error("intercom player stopped")
	}
	if p := r.player(0); len(p.stopped()) != 0 {
		t.Error("radio player stopped")
	}
	// The same voice speaks again soon after the fade, not at the end of
	// the line stopped.
	r.s.SayOnce(paLine("after", alan, log.hooks("after")))
	eventually(t, "after started", func() bool { return log.has("after start") })
	if d := log.when("after start").Sub(stop); d > StopFade+500*time.Millisecond {
		t.Errorf("next PA %v after the stop", d)
	}
	// The intercom ran on: done at its own end, not at the stop.
	eventually(t, "intercom done", func() bool { return log.has("ic done") })
	if log.has("ic done") && log.when("ic done").Sub(log.when("ic start")) < 500*time.Millisecond {
		t.Error("intercom cut by the PA stop")
	}
}

// With OneAtATime a PA line waiting for the floor is dropped by a stop
// without playing; the intercom holding the floor plays on.
func TestStopChannelWaitingForTheFloor(t *testing.T) {
	r := newRig(t, fast, Options{OneAtATime: true})
	log := newHookLog()
	r.s.SayOnce(icLine(strings.Repeat("b", 800), alan, log.hooks("ic")))
	eventually(t, "intercom started", func() bool { return log.has("ic start") })
	r.s.SayOnce(paLine("waiting", ryan, log.hooks("pa")))
	time.Sleep(100 * time.Millisecond) // the PA line waits for the floor
	r.s.StopChannel(ChannelPA)
	eventually(t, "PA done", func() bool { return log.has("pa done") })
	if log.has("pa start") || log.has("ic done") {
		t.Errorf("events %v", log.events())
	}
	// The floor is free after the intercom: a new PA line plays.
	r.s.SayOnce(paLine("then", ryan, log.hooks("then")))
	eventually(t, "then", func() bool { return log.has("then done") && log.has("ic done") })
	if !log.when("then start").After(log.when("ic done").Add(-50 * time.Millisecond)) {
		t.Error("PA over the intercom")
	}
}

// Race: lines with hooks on every channel, stops, gains and closes at
// once. Every OnDone exactly once, every OnStart at most once and before
// it. Run with -race.
func TestHooksRace(t *testing.T) {
	for _, one := range []bool{false, true} {
		t.Run(fmt.Sprint("oneAtATime=", one), func(t *testing.T) {
			r := newRig(t, fast, Options{OneAtATime: one})
			r.s.Set(true, "118.105")
			type count struct{ start, done atomic.Int32 }
			var (
				mu     sync.Mutex
				all    []*count
				faults atomic.Int32
			)
			hooks := func() *Hooks {
				c := &count{}
				mu.Lock()
				all = append(all, c)
				mu.Unlock()
				return &Hooks{
					OnStart: func() {
						if c.done.Load() != 0 || c.start.Add(1) != 1 {
							faults.Add(1)
						}
					},
					OnDone: func() {
						if c.done.Add(1) != 1 {
							faults.Add(1)
						}
					},
				}
			}
			var wg sync.WaitGroup
			for g := range 4 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					rng := rand.New(rand.NewPCG(uint64(g), 7))
					for i := range 60 {
						text := strings.Repeat("x", 5+rng.IntN(40))
						switch rng.IntN(8) {
						case 0:
							r.s.SayOnce(paLine(text, alan, hooks()))
						case 1:
							r.s.SayOnce(icLine(text, ryan, hooks()))
						case 2:
							u := tower(text)
							u.Hooks = hooks()
							r.s.Hear(u)
						case 3:
							r.s.Hear(Utterance{PA: true, Chime: ChimePA, Hooks: hooks()})
						case 4:
							r.s.StopChannel(ChannelPA)
						case 5:
							r.s.StopChannel(ChannelIntercom)
						case 6:
							r.s.SetGain(ChannelPA, rng.Float64())
							_ = r.s.State()
						default:
							u := tower(text)
							u.Hooks = hooks()
							r.s.SayOnce(u)
						}
						if i%20 == 19 {
							time.Sleep(time.Duration(rng.IntN(20)) * time.Millisecond)
						}
					}
				}()
			}
			wg.Wait()
			time.Sleep(100 * time.Millisecond)
			r.s.Close()
			eventually(t, "every OnDone", func() bool {
				mu.Lock()
				defer mu.Unlock()
				for _, c := range all {
					if c.done.Load() != 1 {
						return false
					}
				}
				return true
			})
			time.Sleep(20 * time.Millisecond) // late duplicates would land here
			if n := faults.Load(); n != 0 {
				t.Errorf("%d hooks out of order or repeated", n)
			}
			mu.Lock()
			for _, c := range all {
				if c.done.Load() != 1 || c.start.Load() > 1 {
					t.Errorf("start %d, done %d", c.start.Load(), c.done.Load())
					break
				}
			}
			mu.Unlock()
		})
	}
}
