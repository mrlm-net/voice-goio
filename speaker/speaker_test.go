package speaker

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	voicegoio "github.com/mrlm-net/voice-goio"
)

// The fakes: synthesis is 1 ms of audio per character at 1 kHz, the chain
// and the reading pass it through, the player records what it is given.

const fakeRate = 1000

type fakeTTS struct {
	mu     sync.Mutex
	texts  []string
	voices []voicegoio.VoiceProfile
}

func (f *fakeTTS) Synthesize(_ context.Context, v voicegoio.VoiceProfile, text string) ([]int16, error) {
	f.mu.Lock()
	f.texts = append(f.texts, text)
	f.voices = append(f.voices, v)
	f.mu.Unlock()
	return make([]int16, len(text)), nil
}
func (f *fakeTTS) voicesUsed() []voicegoio.VoiceProfile {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]voicegoio.VoiceProfile(nil), f.voices...)
}
func (f *fakeTTS) SampleRate(voicegoio.VoiceProfile) int { return fakeRate }
func (f *fakeTTS) Close() error                          { return nil }
func (f *fakeTTS) said() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.texts...)
}

type fakePool struct{}

func (f fakePool) AssignCrew(callsign string) voicegoio.VoiceProfile {
	return f.Assign(callsign, voicegoio.Center)
}

func (fakePool) Assign(key string, kind voicegoio.ControllerKind) voicegoio.VoiceProfile {
	return voicegoio.VoiceProfile{Model: key, Radio: string(kind)}
}

// passChain passes the audio through and counts the calls it was put
// through the radio.
type passChain struct {
	mu sync.Mutex
	n  int
}

func (c *passChain) Apply(pcm []int16, _ int, _ string, _ int, _ int64) []int16 {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return pcm
}
func (c *passChain) applied() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

type plainReader struct{}

func (plainReader) Spoken(text string, ph voicegoio.Phraseology) string {
	if ph == voicegoio.FAA {
		return "faa:" + text
	}
	return text
}

type play struct {
	queue     string
	who, text string
	samples   int
	at        time.Time
}

type fakePlayer struct {
	mu     sync.Mutex
	plays  []play
	closed bool
	device string
}

func (p *fakePlayer) Play(t voicegoio.Transmission, pcm []int16, _ int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return voicegoio.ErrClosed
	}
	p.plays = append(p.plays, play{t.Frequency, t.ControllerID, t.Text, len(pcm), time.Now()})
	return nil
}
func (p *fakePlayer) SampleRate() int { return fakeRate }
func (p *fakePlayer) SetDevice(id string) error {
	p.mu.Lock()
	p.device = id
	p.mu.Unlock()
	return nil
}
func (p *fakePlayer) Close() error {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	return nil
}
func (p *fakePlayer) got() []play {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]play(nil), p.plays...)
}

type rig struct {
	s       *Speaker
	tts     *fakeTTS
	chain   *passChain
	mu      sync.Mutex
	players []*fakePlayer
}

func (r *rig) player(i int) *fakePlayer {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i >= len(r.players) {
		return nil
	}
	return r.players[i]
}

var fast = timing{maxLag: time.Second, gap: 20 * time.Millisecond, jitter: 0, atisGap: 100 * time.Millisecond, tick: 5 * time.Millisecond, icGap: 20 * time.Millisecond, pickup: 300 * time.Millisecond}

func newRig(t *testing.T, tm timing, opt Options) *rig {
	t.Helper()
	r := &rig{tts: &fakeTTS{}, chain: &passChain{}}
	opt.Logf = t.Logf
	s := newSpeaker(opt, tm)
	s.openEngine = func() (*engine, error) {
		return &engine{tts: r.tts, backend: "fake", pool: fakePool{}, chain: r.chain, norm: plainReader{}}, nil
	}
	s.newPlayer = func(device string) (player, []voicegoio.Device, error) {
		p := &fakePlayer{device: device}
		r.mu.Lock()
		r.players = append(r.players, p)
		r.mu.Unlock()
		return p, []voicegoio.Device{{ID: "", Name: "default", Default: true}, {ID: "1", Name: "headset"}}, nil
	}
	r.s = s
	go s.run()
	t.Cleanup(func() { s.Close() })
	return r
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func tower(text string) Utterance {
	return Utterance{Airport: "LKPR", Position: PosTower, Callsign: "CSA123", Frequency: "118.105", Text: text}
}

// Nothing is said without a frequency, off it, while off, or an ATIS.
func TestHearOnlyTheFrequencyFollowed(t *testing.T) {
	r := newRig(t, fast, Options{})
	r.s.Hear(tower("a")) // off
	r.s.Set(true, "")
	r.s.Hear(tower("b")) // no frequency
	r.s.Set(true, "118.105")
	r.s.Hear(Utterance{Airport: "LKPR", Position: PosTower, Frequency: "120.530", Text: "c"})
	r.s.Hear(Utterance{Airport: "LKPR", Position: PosATIS, Frequency: "118.105", Text: "d"})
	r.s.Hear(tower("e"))
	// The first player went with the change of frequency.
	if !r.player(0).isClosed() || r.player(1) == nil {
		t.Fatal("player not reset")
	}
	eventually(t, "e said", func() bool { return len(r.player(1).got()) == 1 })
	time.Sleep(50 * time.Millisecond)
	if got := r.player(1).got(); len(got) != 1 || got[0].text != "e" || got[0].who != PosTower {
		t.Fatalf("played %+v", got)
	}
	if st := r.s.State(); !st.On || st.Status != "on (fake)" || st.Frequency != "118.105" || len(st.Devices) != 2 {
		t.Errorf("state %+v", st)
	}
}

// One at a time: the next starts after the last has been said and the gap.
func TestOneAtATimeWithGap(t *testing.T) {
	r := newRig(t, fast, Options{})
	r.s.Set(true, "118.105")
	r.s.Hear(tower("CSA123, cleared to land"))
	pilot := tower("cleared to land, CSA123")
	pilot.Pilot = true
	r.s.Hear(pilot)
	eventually(t, "both said", func() bool { return len(r.player(0).got()) == 2 })
	got := r.player(0).got()
	if got[0].who != PosTower || got[1].who != "CSA123" {
		t.Errorf("who: %q, %q", got[0].who, got[1].who)
	}
	first := time.Duration(got[0].samples) * time.Second / fakeRate
	if d := got[1].at.Sub(got[0].at); d < first+fast.gap {
		t.Errorf("second after %v, want at least %v (said) + %v (gap)", d, first, fast.gap)
	}
	// Synthesised with a full stop, padded with TailPad.
	if want := len(SpokenEnd(got[0].text)) + int(TailPad.Seconds()*fakeRate); got[0].samples != want {
		t.Errorf("%d samples, want %d", got[0].samples, want)
	}
	if s := r.tts.said(); s[0] != "CSA123, cleared to land." {
		t.Errorf("synthesised %q", s[0])
	}
}

// What waited longer than the lag is dropped.
func TestLateIsDropped(t *testing.T) {
	r := newRig(t, fast, Options{})
	r.s.Set(true, "118.105")
	r.s.mu.Lock()
	r.s.queue <- item{tower("stale"), time.Now().Add(-2 * fast.maxLag)}
	r.s.mu.Unlock()
	r.s.Hear(tower("fresh"))
	eventually(t, "fresh said", func() bool { return len(r.player(0).got()) == 1 })
	time.Sleep(50 * time.Millisecond)
	if got := r.player(0).got(); len(got) != 1 || got[0].text != "fresh" {
		t.Fatalf("played %+v", got)
	}
}

// A new frequency closes the player and drops what was queued.
func TestFrequencyChangeResets(t *testing.T) {
	tm := fast
	tm.gap = time.Hour // nothing after the first is said
	r := newRig(t, tm, Options{})
	r.s.Set(true, "118.105")
	r.s.Hear(tower("one"))
	r.s.Hear(tower("two"))
	r.s.Hear(tower("three"))
	eventually(t, "one said", func() bool { return len(r.player(0).got()) == 1 })
	r.s.Set(true, "120.530")
	if !r.player(0).isClosed() {
		t.Error("old player not closed")
	}
	if n := len(r.s.queue); n != 0 {
		t.Errorf("%d still queued", n)
	}
	if r.player(1) == nil {
		t.Fatal("no new player")
	}
	r.s.Set(false, "120.530")
	if !r.player(1).isClosed() || r.s.State().Status != "off" {
		t.Error("off left the player open")
	}
}

// The ATIS is synthesised once and loops with the gap; a new text anew.
func TestATISLoops(t *testing.T) {
	r := newRig(t, fast, Options{})
	text := "Ruzyne information A"
	r.s.SetATIS("122.155", "LKPR", text)
	r.s.Set(true, "122.155")
	eventually(t, "two runs", func() bool { return len(r.player(0).got()) >= 2 })
	got := r.player(0).got()
	if got[0].who != "atis" {
		t.Errorf("who %q", got[0].who)
	}
	full := len(SpokenEnd(text)) + int(TailPad.Seconds()*fakeRate)
	if got[1].samples != full {
		t.Errorf("second run %d samples, want the whole %d", got[1].samples, full)
	}
	period := time.Duration(full)*time.Second/fakeRate + fast.atisGap
	if d := got[1].at.Sub(got[0].at); d < period-10*time.Millisecond {
		t.Errorf("runs %v apart, want %v", d, period)
	}
	if n := len(r.tts.said()); n != 1 {
		t.Errorf("synthesised %d times", n)
	}
	r.s.SetATIS("122.155", "LKPR", "Ruzyne information B")
	eventually(t, "B", func() bool { return len(r.tts.said()) == 2 })
}

// Tuning in joins the broadcast where it is, mid-sentence.
func TestATISJoinsMidSentence(t *testing.T) {
	r := newRig(t, fast, Options{})
	text := strings.Repeat("x", 600) // 0.6 s + pad
	r.s.SetATIS("122.155", "LKPR", text)
	r.s.mu.Lock()
	r.s.atisText = text
	r.s.atisPCM = make([]int16, 750)
	r.s.atisStart = time.Now().Add(-300 * time.Millisecond)
	r.s.mu.Unlock()
	r.s.Set(true, "122.155")
	eventually(t, "joined", func() bool { return len(r.player(0).got()) >= 1 })
	if n := r.player(0).got()[0].samples; n >= 750 || n < 300 {
		t.Errorf("joined with %d of 750 samples", n)
	}
	if len(r.tts.said()) != 0 {
		t.Error("synthesised again")
	}
}

// Options.ATIS is asked for the ATIS on the frequency.
func TestATISSource(t *testing.T) {
	r := newRig(t, fast, Options{ATIS: func(freq string) (string, string, bool) {
		return "KJFK", "Kennedy information C", freq == "128.725"
	}})
	r.s.Set(true, "128.725")
	eventually(t, "said", func() bool { return len(r.player(0).got()) >= 1 })
}

// SayOnce speaks while off; FAA is read the FAA way; OnSay is told.
func TestSayOnce(t *testing.T) {
	var heard sync.WaitGroup
	heard.Add(1)
	r := newRig(t, fast, Options{OnSay: func(u Utterance) { heard.Done() }})
	u := Utterance{Airport: "KJFK", Position: PosATIS, Text: "Kennedy information C", Phraseology: voicegoio.FAA}
	if !r.s.SayOnce(u) {
		t.Fatal("not said")
	}
	heard.Wait()
	eventually(t, "said", func() bool { return r.player(0) != nil && len(r.player(0).got()) == 1 })
	if s := r.tts.said(); s[0] != "faa:Kennedy information C." {
		t.Errorf("synthesised %q", s[0])
	}
	if r.s.State().On {
		t.Error("turned on")
	}
}

func TestNoVoiceStatus(t *testing.T) {
	r := newRig(t, fast, Options{})
	r.s.openEngine = func() (*engine, error) { return nil, ErrNoVoice }
	r.s.Set(true, "118.105")
	if st := r.s.State(); st.On || st.Status != ErrNoVoice.Error() {
		t.Errorf("state %+v", st)
	}
	if r.s.SayOnce(tower("x")) {
		t.Error("said without a voice")
	}
}

// Without piper the real opening reports ErrNoVoice with the hint.
func TestOpenPiperMissing(t *testing.T) {
	_, err := openPiper(Options{PiperPath: filepath.Join(t.TempDir(), "piper.exe"), VoicesDir: t.TempDir(), Hint: "see the README"})
	if !errors.Is(err, ErrNoVoice) || !strings.HasSuffix(err.Error(), "(see the README)") {
		t.Fatalf("err %v", err)
	}
}

func TestSetDevice(t *testing.T) {
	r := newRig(t, fast, Options{Device: "2"})
	if err := r.s.SetDevice("1"); err != nil {
		t.Fatal(err)
	}
	r.s.Set(true, "118.105")
	if r.player(0).dev() != "1" || r.s.State().Device != "1" {
		t.Errorf("device %q", r.player(0).dev())
	}
	r.s.SetDevice("")
	if r.player(0).dev() != "" {
		t.Error("player not switched")
	}
}

// A controller hands over after a shift; a crew keeps its voice.
func TestShifts(t *testing.T) {
	r := newRig(t, fast, Options{})
	if k := r.s.onShift("LKPR", PosTower); k != "LKPR" {
		t.Errorf("first shift %q", k)
	}
	r.s.mu.Lock()
	r.s.shifts["LKPR/"+PosTower].until = time.Now().Add(-time.Second)
	r.s.mu.Unlock()
	if k := r.s.onShift("LKPR", PosTower); k != "LKPR-2" {
		t.Errorf("second shift %q", k)
	}
	e, _ := r.s.openEngine()
	p := Utterance{Airport: "LKPR", Position: PosTower, Callsign: "CSA123", Pilot: true}
	if v := r.s.voiceOf(e, p); v.Model != "CSA123" || v.Radio != string(voicegoio.Center) {
		t.Errorf("pilot voice %+v", v)
	}
	if v := r.s.voiceOf(e, Utterance{Airport: "LKPR", Position: PosGround}); v.Model != "LKPR" || v.Radio != string(voicegoio.Ground) {
		t.Errorf("ground voice %+v", v)
	}
	// One controller working ground and tower: one voice, each frequency's
	// radio sound; another controller another voice.
	g := r.s.voiceOf(e, Utterance{Airport: "LKPR", Position: PosGround, Controller: "LKPR twr"})
	tw := r.s.voiceOf(e, Utterance{Airport: "LKPR", Position: PosTower, Controller: "LKPR twr"})
	other := r.s.voiceOf(e, Utterance{Airport: "LKPR", Position: PosTower, Controller: "LKPR twr2"})
	if g.Model != tw.Model || g.Radio != string(voicegoio.Ground) || tw.Radio != string(voicegoio.Tower) || other.Model == tw.Model {
		t.Errorf("controller voices: ground %+v, tower %+v, other %+v", g, tw, other)
	}
	if who(Utterance{Position: PosGround, Controller: "LKPR twr"}) != "LKPR twr" {
		t.Error("who is not the controller")
	}
}

func TestKindOf(t *testing.T) {
	for pos, want := range map[string]voicegoio.ControllerKind{
		PosDelivery: voicegoio.Ground, PosGround: voicegoio.Ground, PosTower: voicegoio.Tower,
		PosApproach: voicegoio.Approach, PosDeparture: voicegoio.Approach, PosCenter: voicegoio.Center,
		PosATIS: voicegoio.ATIS, "": voicegoio.Tower,
	} {
		if got := KindOf(pos); got != want {
			t.Errorf("%q: %q, want %q", pos, got, want)
		}
	}
}

func TestClip(t *testing.T) {
	r := newRig(t, fast, Options{})
	pcm, rate, err := r.s.Clip(tower("CSA123, roger"))
	if err != nil || rate != fakeRate || len(pcm) != len("CSA123, roger") {
		t.Fatalf("clip %d samples at %d: %v", len(pcm), rate, err)
	}
	if b := WAV(pcm, rate); string(b[:4]) != "RIFF" || len(b) != 44+2*len(pcm) {
		t.Errorf("wav %d bytes", len(b))
	}
}

// A call is spoken to its end: a full stop after the call sign (piper cuts
// an open sentence's last syllable), silence after the audio.
func TestSpokenEnd(t *testing.T) {
	for in, want := range map[string]string{
		"Runway 24, cleared to land, Wizzair 1387": "Runway 24, cleared to land, Wizzair 1387.",
		"Say again?": "Say again?", "Roger.": "Roger.", "": "",
	} {
		if got := SpokenEnd(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	if n := len(Pad(make([]int16, 100), 20000)); n != 100+3000 {
		t.Errorf("padded to %d samples", n)
	}
}

func (p *fakePlayer) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

func (p *fakePlayer) dev() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.device
}

// Tempo speeds a voice up (a lower LengthScale) within MinTempo…MaxTempo;
// 0 and 1 leave its pace.
func TestVoiceTempo(t *testing.T) {
	r := newRig(t, fast, Options{})
	e, _ := r.s.openEngine()
	u := Utterance{Airport: "LKPR", Position: PosTower}
	base := r.s.voiceOf(e, u)
	ls := base.LengthScale
	if ls <= 0 {
		ls = 1
	}
	for _, c := range []struct{ tempo, want float32 }{{0, base.LengthScale}, {1, base.LengthScale}, {1.25, ls / 1.25}, {3, ls / MaxTempo}} {
		u.Tempo = c.tempo
		if got := r.s.voiceOf(e, u).LengthScale; math.Abs(float64(got-c.want)) > 1e-4 {
			t.Errorf("tempo %.2f: length scale %.3f, want %.3f", c.tempo, got, c.want)
		}
	}
}
