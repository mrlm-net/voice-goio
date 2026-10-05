package speaker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"time"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/audio"
	"github.com/mrlm-net/voice-goio/audio/radio"
	"github.com/mrlm-net/voice-goio/internal/wav"
	"github.com/mrlm-net/voice-goio/normalise"
	"github.com/mrlm-net/voice-goio/tts"
	"github.com/mrlm-net/voice-goio/tts/piper"
	"github.com/mrlm-net/voice-goio/voices"
)

// The rules of the radio, as the airport map has them (#419, #462).
const (
	// MaxLag: an utterance not yet said this long after it was heard is
	// dropped, so a busy frequency stays live rather than minutes behind.
	MaxLag = 60 * time.Second
	// QueueKey is the one player queue everything goes through: one
	// frequency, one thing at a time, as on a single receiver.
	QueueKey = "radio"
	// Gap and up to GapJitter more, at random, is the pause between two
	// transmissions (1 to 5 s); halved while more than two wait.
	Gap       = time.Second
	GapJitter = 4 * time.Second
	// ATISGap is the silence between two runs of an ATIS broadcast.
	ATISGap = 3 * time.Second
	// Tick is how often the speaker looks for an ATIS to broadcast and,
	// while waiting, for a change of frequency.
	Tick = 100 * time.Millisecond
	// TailPad is the silence after each call before the radio effect, so
	// the chain's fade does not take the last syllable.
	TailPad = 150 * time.Millisecond
	// A station's controller hands over every ShiftMin to ShiftMax (a new
	// voice on the frequency); a crew keeps its voice for good.
	ShiftMin = 30 * time.Minute
	ShiftMax = 60 * time.Minute
	// ExcludedVoice was never assigned before v0.7.0: the Czech model read
	// English through the US phonemizer badly. Through RP phonemes it is the
	// light Czech accent of a controller at a Czech airport (LK), the words
	// pronounced as every English voice says them; an application that does
	// not want it lists it in Options.Exclude.
	ExcludedVoice = "cs_CZ-jirka-medium"
	// FemaleShare is the share of positions given a female voice (1:8).
	FemaleShare = 1.0 / 9
	// IntercomKey is the player queue of the intercom: the cockpit and the
	// cabin, heard beside the radio, never through it.
	IntercomKey = "intercom"
	// IntercomGap is the pause between two intercom utterances (a challenge
	// and its response), synthesis included.
	IntercomGap = 400 * time.Millisecond
)

// Positions, as Utterance.Position takes them (the values of simconnect's
// traffic.Position).
const (
	PosDelivery  = "delivery"
	PosGround    = "ground"
	PosTower     = "tower"
	PosApproach  = "approach"
	PosDeparture = "departure"
	PosCenter    = "center"
	PosATIS      = "atis"
)

// ErrNoVoice: piper (or a voice model) is missing; the speaker stays silent.
var ErrNoVoice = errors.New("no voice: piper and a voice model are needed")

// Utterance is one transmission to be said.
type Utterance struct {
	// Airport is the station's airport (ICAO): it picks the voices of its
	// region and keys the controller's shift.
	Airport string
	// Position is the controller's position (PosTower, …): its voice and
	// radio sound. PosATIS utterances are not taken by Hear (the ATIS is a
	// broadcast: SetATIS); SayOnce says them.
	Position string
	// Controller is who works the frequency ("" the position's controller):
	// the same Controller is the same person, so the same voice, on every
	// frequency it works (ground and tower combined), with the radio sound
	// of Position.
	Controller string
	// Callsign is the aircraft spoken to or from.
	Callsign string
	// Pilot: said by the crew, in Callsign's voice.
	Pilot bool
	// Frequency is where it is said, written as the app writes the
	// frequency it follows (compared as strings).
	Frequency string
	// Text in the application's normal tokens; normalised before synthesis.
	Text string
	// Phraseology is the reading of numbers and frequencies: voicegoio.FAA
	// at a US airport, voicegoio.ICAO ("" too) elsewhere.
	Phraseology voicegoio.Phraseology
	// Voice, when set, is the voice it is said in (a crew member's voice the
	// player chose: voices.Model.Profile), winning over the pool's pick; even
	// ExcludedVoice is said if asked for. A Radio left "" is the position's
	// (Center for a pilot); prosody left zero is piper's default.
	Voice *voicegoio.VoiceProfile
	// Intercom: said in the cockpit or the cabin, not on the radio. No radio
	// chain (no band-pass, noise or squelch), not tied to the frequency
	// followed: Hear queues it even while the speaker is off or follows
	// another frequency (or none), and a change of frequency does not drop
	// it. It plays on its own player queue (IntercomKey), one at a time with
	// IntercomGap between, beside the radio. Frequency is ignored.
	Intercom bool
	// Chime, when set, makes it that chime (ChimePCM) instead of speech:
	// played on the intercom in order (Text and Voice ignored); a ChimeCall
	// is followed by a pickup pause (PickupMin…+PickupJitter).
	Chime Chime
	// PA: said on the cabin PA (SayPA): its own queue and player, through
	// the cabin speaker chain (PAChain); not tied to the frequency.
	PA bool
}

// Options configures a Speaker.
type Options struct {
	// PiperPath is the piper executable; "" bin/piper/piper.exe next to the
	// running executable.
	PiperPath string
	// VoicesDir is the folder of voice models; "" the per-user data folder
	// (%LOCALAPPDATA%\voice-goio\voices on Windows).
	VoicesDir string
	// Device is the output at start ("" the system default).
	Device string
	// Hint is appended to ErrNoVoice in the status, e.g. "see the README".
	Hint string
	// Exclude lists voice models never assigned to a position (an explicit
	// Utterance.Voice still speaks); none by default.
	Exclude []string
	// Accents lets controllers speak with their airport's accent (the
	// Czech, German, Dutch, Polish, French, Italian and Spanish models on
	// English phonemes); off by default: English voices only, until the
	// accent models are trained for English.
	Accents bool
	// ATIS, when set, is asked every Tick for the ATIS on the frequency
	// followed (its airport and text); otherwise SetATIS's are used.
	ATIS func(freq string) (airport, text string, ok bool)
	// OnSay is called just before an utterance plays (a camera cutting to
	// the aircraft heard). It must not block.
	OnSay func(Utterance)
	// Logf logs synthesis errors; nil log.Printf.
	Logf func(format string, args ...any)
}

// State is what a UI shows of the speaker.
type State struct {
	On        bool   `json:"on"`
	Frequency string `json:"frequency"`
	// Status is why it is silent ("off", the error) or "on (piper)".
	Status  string `json:"status"`
	Backend string `json:"backend,omitempty"`
	// Device is the output picked ("" the system default), Devices those
	// there are (known once the sound has been on).
	Device string `json:"device"`
	// DeviceFor is each channel's own output (SetDeviceFor); a channel not
	// in it plays on Device.
	DeviceFor map[Channel]string `json:"deviceFor,omitempty"`
	Devices   []voicegoio.Device `json:"devices,omitempty"`
}

// The pipeline behind the speaker, as small interfaces so the rules can be
// tested without piper or a sound card.
type (
	synth interface {
		Synthesize(ctx context.Context, profile voicegoio.VoiceProfile, spokenText string) ([]int16, error)
		SampleRate(profile voicegoio.VoiceProfile) int
		Close() error
	}
	voicer interface {
		Assign(key string, kind voicegoio.ControllerKind) voicegoio.VoiceProfile
		AssignCrew(callsign string) voicegoio.VoiceProfile
	}
	effect interface {
		Apply(pcm []int16, inRate int, profile string, outRate int, seed int64) []int16
	}
	reader interface {
		Spoken(text string, ph voicegoio.Phraseology) string
	}
	player interface {
		Play(t voicegoio.Transmission, pcm []int16, sampleRate int) error
		SampleRate() int
		SetDevice(id string) error
		Close() error
	}
)

type engine struct {
	tts     synth
	backend string
	pool    voicer
	chain   effect
	norm    reader
}

// timing holds the constants above; tests shorten them.
type timing struct {
	maxLag, gap, jitter, atisGap, tick, icGap time.Duration
	pickup, pickupJitter                      time.Duration // after a ChimeCall
}

type item struct {
	u    Utterance
	when time.Time
}

type atisInfo struct{ airport, text string }

type shift struct {
	n     int
	until time.Time
}

// Speaker says what is heard on one frequency, one thing at a time. It is
// safe for concurrent use.
type Speaker struct {
	opt Options
	t   timing

	mu      sync.Mutex
	on      bool
	freq    string // followed; "" nothing is said
	status  string
	backend string
	device  string
	devices []voicegoio.Device
	eng     *engine
	player  player

	queue chan item
	// The ATIS broadcast: its text, its audio, when its loop started.
	atisText  string
	atisPCM   []int16
	atisStart time.Time
	atis      map[string]atisInfo // SetATIS, by frequency
	// When the last transmission ends and who it was to or from.
	lastEnd time.Time
	lastCS  string
	shifts  map[string]*shift
	rng     *rand.Rand
	// The intercom and the cabin PA: each its own queue and player,
	// untouched by Set, playing in order with itself, beside the radio.
	ic, pa *lane
	// devFor: a channel's own output (SetDeviceFor); none: the main one.
	devFor map[Channel]string
	// mouths: when each voice (voiceKey) is free again, across the lanes.
	mouths map[string]time.Time

	openEngine func() (*engine, error)
	newPlayer  func(device string) (player, []voicegoio.Device, error)
	done       chan struct{}
	closeOnce  sync.Once
}

// New makes a speaker, off; nothing is opened until it is turned on.
func New(opt Options) *Speaker {
	s := newSpeaker(opt, timing{maxLag: MaxLag, gap: Gap, jitter: GapJitter, atisGap: ATISGap, tick: Tick, icGap: IntercomGap, pickup: PickupMin, pickupJitter: PickupJitter})
	s.openEngine = func() (*engine, error) { return openPiper(s.opt) }
	s.newPlayer = openPlayer
	go s.run()
	return s
}

func newSpeaker(opt Options, t timing) *Speaker {
	if opt.Logf == nil {
		opt.Logf = log.Printf
	}
	return &Speaker{
		opt: opt, t: t, status: "off", device: opt.Device,
		queue:  make(chan item, 64),
		ic:     newLane(ChannelIntercom, IntercomKey),
		pa:     newLane(ChannelPA, PAKey),
		devFor: map[Channel]string{},
		mouths: map[string]time.Time{},
		atis:   map[string]atisInfo{},
		rng:    rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0x70ce)),
		done:   make(chan struct{}),
	}
}

// openPiper opens piper with the default voice pool.
func openPiper(opt Options) (*engine, error) {
	e, backend, err := tts.Open(tts.Options{Piper: piper.Options{PiperPath: opt.PiperPath, VoicesDir: opt.VoicesDir}})
	if err != nil {
		return nil, err
	}
	if backend != tts.BackendPiper {
		e.Close()
		if opt.Hint != "" {
			return nil, fmt.Errorf("%w (%s)", ErrNoVoice, opt.Hint)
		}
		return nil, ErrNoVoice
	}
	man, err := voices.LoadDefault()
	if err != nil {
		e.Close()
		return nil, err
	}
	exclude := opt.Exclude
	if !opt.Accents {
		exclude = append(append([]string(nil), exclude...), voices.AccentModels(man)...)
	}
	pool := voices.NewPool(man, voices.PoolOptions{Seed: time.Now().UnixNano(), AllowUnaudited: true, Dir: opt.VoicesDir, Exclude: exclude, FemaleShare: FemaleShare})
	return &engine{tts: e, backend: backend, pool: pool, chain: radio.Default(), norm: normalise.New()}, nil
}

func openPlayer(device string) (player, []voicegoio.Device, error) {
	p, err := audio.NewPlayer(audio.Options{DeviceID: device})
	if err != nil {
		return nil, nil, err
	}
	d, _ := p.Devices()
	go func() {
		for range p.Events() { // drained: the player needs it
		}
	}()
	return p, d, nil
}

// open starts the pipeline on first use; s.mu held.
func (s *Speaker) open() error {
	if err := s.openEngineLocked(); err != nil {
		return err
	}
	if s.player == nil {
		p, d, err := s.newPlayer(s.deviceOf(ChannelRadio))
		if err != nil {
			return err
		}
		if d != nil {
			s.devices = d
		}
		s.player = p
	}
	return nil
}

// openEngineLocked opens the voices without a player; s.mu held.
func (s *Speaker) openEngineLocked() error {
	if s.eng != nil {
		return nil
	}
	e, err := s.openEngine()
	if err != nil {
		return err
	}
	s.eng, s.backend = e, e.backend
	return nil
}

// Open opens the voices (piper and its models) without a player, for Clip.
func (s *Speaker) Open() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.openEngineLocked()
}

// Set turns the speaker on or off and picks the frequency followed: a new
// frequency resets the player, and what was queued for the last one goes.
func (s *Speaker) Set(on bool, freq string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if freq != s.freq && s.player != nil {
		s.player.Close()
		s.player = nil
		for len(s.queue) > 0 {
			<-s.queue
		}
	}
	s.freq = freq
	if !on {
		s.on, s.status = false, "off"
		if s.player != nil {
			s.player.Close() // what is queued goes with it
			s.player = nil
		}
		return
	}
	if err := s.open(); err != nil {
		s.on, s.status = false, err.Error()
		s.opt.Logf("voice: %v", err)
		return
	}
	s.on, s.status = true, "on ("+s.backend+")"
}

// State is the speaker's state.
func (s *Speaker) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	devFor := map[Channel]string{}
	for ch, id := range s.devFor {
		devFor[ch] = id
	}
	return State{On: s.on, Frequency: s.freq, Status: s.status, Backend: s.backend, Device: s.device, DeviceFor: devFor, Devices: append([]voicegoio.Device(nil), s.devices...)}
}

// Devices are the outputs there are (known once the sound has been on).
func (s *Speaker) Devices() []voicegoio.Device { return s.State().Devices }

// SetDevice plays on output id ("" the system default) from now on: every
// channel without its own (SetDeviceFor).
func (s *Speaker) SetDevice(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == s.device {
		return nil
	}
	old := s.device
	s.device = id
	for _, ch := range []Channel{ChannelRadio, ChannelIntercom, ChannelPA} {
		if s.devFor[ch] != "" {
			continue
		}
		if p := s.playerOf(ch); p != nil {
			if err := p.SetDevice(id); err != nil {
				s.device = old
				return err
			}
		}
	}
	return nil
}

// SetDeviceFor plays channel ch on output id from now on; "" follows the
// main device (SetDevice) again.
func (s *Speaker) SetDeviceFor(ch Channel, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		delete(s.devFor, ch)
	} else {
		s.devFor[ch] = id
	}
	if p := s.playerOf(ch); p != nil {
		return p.SetDevice(s.deviceOf(ch))
	}
	return nil
}

// deviceOf is the output channel ch plays on; s.mu held.
func (s *Speaker) deviceOf(ch Channel) string {
	if id := s.devFor[ch]; id != "" {
		return id
	}
	return s.device
}

// playerOf is channel ch's player, nil before it is open; s.mu held.
func (s *Speaker) playerOf(ch Channel) player {
	switch ch {
	case ChannelIntercom:
		return s.ic.player
	case ChannelPA:
		return s.pa.player
	}
	return s.player
}

// Hear takes an utterance from the radio: queued if the speaker is on and
// it is on the frequency followed (nothing without one). An Intercom
// utterance is queued on the intercom whatever the speaker's state. It never
// blocks; a full queue drops it.
func (s *Speaker) Hear(u Utterance) {
	if u.PA {
		s.pa.queueItem(u)
		return
	}
	if u.Intercom {
		s.ic.queueItem(u)
		return
	}
	s.mu.Lock()
	skip := !s.on || s.freq == "" || u.Frequency != s.freq || u.Position == PosATIS
	s.mu.Unlock()
	if skip {
		return
	}
	select {
	case s.queue <- item{u, time.Now()}:
	default: // behind: drop it
	}
}

// SayOnce says u now, whatever the frequency followed and even while off
// (an airport panel's ATIS button); false when the voice is unavailable
// (State().Status says why). An Intercom utterance is queued on the
// intercom, as SayIntercom.
func (s *Speaker) SayOnce(u Utterance) bool {
	if u.PA {
		return s.onLane(s.pa, u)
	}
	if u.Intercom {
		return s.onLane(s.ic, u)
	}
	s.mu.Lock()
	if !s.on {
		if err := s.open(); err != nil {
			s.status = err.Error()
			s.mu.Unlock()
			return false
		}
	}
	p := s.player
	s.mu.Unlock()
	if p == nil {
		return false
	}
	go s.say(u, true)
	return true
}

// SayIntercom says text on the intercom in voice: the cockpit voice, without
// the radio chain, whatever the frequency followed and even while the radio
// is off. Queued behind what the intercom is saying; false when the voice is
// unavailable (State().Status says why while the radio is off; logged).
func (s *Speaker) SayIntercom(text string, voice voicegoio.VoiceProfile) bool {
	return s.onLane(s.ic, Utterance{Intercom: true, Text: text, Voice: &voice})
}

// SayPA says text on the cabin PA in voice, through the cabin speaker
// chain (PAChain): its own queue and player, in order with itself and
// beside the intercom, so a PA and an intercom call overlap as in an
// aircraft.
func (s *Speaker) SayPA(text string, voice voicegoio.VoiceProfile) bool {
	return s.onLane(s.pa, Utterance{PA: true, Text: text, Voice: &voice})
}

// Chime queues c in order with what is said where it belongs: ChimePA on
// the PA, the call and the seat-belt chime on the intercom; false when it
// cannot play (no output device) or c is unknown.
func (s *Speaker) Chime(c Chime) bool {
	if ChimePCM(c) == nil {
		return false
	}
	if c == ChimePA {
		return s.onLane(s.pa, Utterance{PA: true, Chime: c})
	}
	return s.onLane(s.ic, Utterance{Intercom: true, Chime: c})
}

// Channel is where a speaker plays: the radio, the intercom (cockpit and
// cabin calls) or the cabin PA, each on its own output if set
// (SetDeviceFor).
type Channel string

const (
	ChannelRadio    Channel = "radio"
	ChannelIntercom Channel = "intercom"
	ChannelPA       Channel = "pa"
)

// PAKey is the player queue of the cabin PA.
const PAKey = "pa"

// lane is the intercom or the PA: a queue said one at a time on its own
// player.
type lane struct {
	ch    Channel
	key   string
	queue chan item
	// guarded by Speaker.mu:
	player   player
	lastEnd  time.Time
	chimeEnd time.Time // when the last chime (and its pickup) ended
}

func newLane(ch Channel, key string) *lane {
	return &lane{ch: ch, key: key, queue: make(chan item, 64)}
}

// onLane opens the voice (unless u is a chime) and l's player, and queues
// u on l.
func (s *Speaker) onLane(l *lane, u Utterance) bool {
	u.Intercom, u.PA = l.ch == ChannelIntercom, l.ch == ChannelPA
	s.mu.Lock()
	err := s.openLaneLocked(l, u.Chime == "")
	if err != nil {
		if !s.on {
			s.status = err.Error()
		}
		s.mu.Unlock()
		s.opt.Logf("voice: %s: %v", l.ch, err)
		return false
	}
	s.mu.Unlock()
	return l.queueItem(u)
}

// queueItem queues u on l without blocking; false when the queue is full
// (u is dropped).
func (l *lane) queueItem(u Utterance) bool {
	select {
	case l.queue <- item{u, time.Now()}:
		return true
	default: // behind: drop it
		return false
	}
}

// openLaneLocked opens the voices (when voice: a chime needs none) and l's
// player; s.mu held.
func (s *Speaker) openLaneLocked(l *lane, voice bool) error {
	select {
	case <-s.done:
		return voicegoio.ErrClosed
	default:
	}
	if voice {
		if err := s.openEngineLocked(); err != nil {
			return err
		}
	}
	if l.player == nil {
		p, d, err := s.newPlayer(s.deviceOf(l.ch))
		if err != nil {
			return err
		}
		if d != nil {
			s.devices = d
		}
		l.player = p
	}
	return nil
}

// runLane says what is queued on l, one at a time.
func (s *Speaker) runLane(l *lane) {
	for {
		select {
		case <-s.done:
			return
		case it := <-l.queue:
			// Behind a chime, late from its end (with the pickup): the
			// answer to a call is not stale for having waited for it.
			from := it.when
			s.mu.Lock()
			if l.chimeEnd.After(from) {
				from = l.chimeEnd
			}
			s.mu.Unlock()
			if time.Since(from) > s.t.maxLag {
				continue
			}
			s.sayOnLane(l, it.u)
		}
	}
}

// sayOnLane synthesises u in its voice, without the radio chain (the PA
// through the cabin speaker's), and waits while it is played on l.
func (s *Speaker) sayOnLane(l *lane, u Utterance) {
	s.mu.Lock()
	err := s.openLaneLocked(l, u.Chime == "")
	e, p, next := s.eng, l.player, l.lastEnd.Add(s.t.icGap)
	s.mu.Unlock()
	if err != nil {
		s.opt.Logf("voice: %s: %v", l.ch, err)
		return
	}
	if u.Chime != "" {
		s.playChime(l, u, p, next)
		return
	}
	voice := s.voiceOf(e, u)
	ph := phraseology(u.Phraseology)
	pcm, err := e.tts.Synthesize(context.Background(), voice, SpokenEnd(e.norm.Spoken(u.Text, ph)))
	if err != nil {
		s.opt.Logf("voice: %s: %v", l.ch, err)
		return
	}
	rate := e.tts.SampleRate(voice)
	if u.PA {
		pcm = PAChain(pcm, rate)
	}
	out := Pad(pcm, rate) // the player converts the rate
	// One voice says one line at a time, whatever the channel: a captain on
	// the intercom does not make a PA meanwhile, it waits for the line to
	// end; other voices may overlap. The start is reserved at once, so two
	// lanes never take the same moment.
	said := samplesDuration(len(out), rate)
	s.mu.Lock()
	start := time.Now()
	if next.After(start) {
		start = next
	}
	key := voiceKey(voice)
	if busy := s.mouths[key]; busy.After(start) {
		start = busy
	}
	s.mouths[key] = start.Add(said)
	s.mu.Unlock()
	if !s.sleep(time.Until(start)) {
		return
	}
	if s.opt.OnSay != nil {
		s.opt.OnSay(u)
	}
	if err := p.Play(voicegoio.Transmission{Frequency: l.key, ControllerID: who(u), Phraseology: ph, Text: u.Text}, out, rate); err != nil {
		return // closed meanwhile
	}
	s.mu.Lock()
	l.lastEnd = time.Now().Add(said)
	s.mu.Unlock()
	s.sleep(said)
}

// sleep waits d unless the speaker is closed meanwhile, and reports whether
// it was not.
func (s *Speaker) sleep(d time.Duration) bool {
	if d <= 0 {
		return true
	}
	select {
	case <-s.done:
		return false
	case <-time.After(d):
		return true
	}
}

// who is the player's ControllerID for u: the pilot's call sign, else the
// position.
func who(u Utterance) string {
	if u.Pilot {
		return u.Callsign
	}
	if u.Controller != "" {
		return u.Controller
	}
	return u.Position
}

// SetATIS sets the ATIS broadcast on freq: airport's text (with its
// information letter: a new text is synthesised anew). "" text ends it.
// Options.ATIS, when set, is used instead.
func (s *Speaker) SetATIS(freq, airport, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if text == "" {
		delete(s.atis, freq)
		return
	}
	s.atis[freq] = atisInfo{airport, text}
}

func (s *Speaker) atisOn(freq string) (string, string, bool) {
	if s.opt.ATIS != nil {
		return s.opt.ATIS(freq)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.atis[freq]
	return a.airport, a.text, ok
}

// Close stops the speaker and releases the player and piper.
func (s *Speaker) Close() error {
	s.closeOnce.Do(func() { close(s.done) })
	s.mu.Lock()
	defer s.mu.Unlock()
	s.on, s.status = false, "off"
	if s.player != nil {
		s.player.Close()
		s.player = nil
	}
	for _, l := range []*lane{s.ic, s.pa} {
		if l.player != nil {
			l.player.Close()
			l.player = nil
		}
	}
	if s.eng != nil {
		s.eng.tts.Close()
		s.eng = nil
	}
	return nil
}

// run says what is queued; while the followed frequency is an ATIS and
// nothing else is to be said, it says the ATIS again and again. The intercom
// runs beside it.
func (s *Speaker) run() {
	go s.runLane(s.ic)
	go s.runLane(s.pa)
	for {
		select {
		case <-s.done:
			return
		case it := <-s.queue:
			if time.Since(it.when) > s.t.maxLag {
				continue
			}
			s.say(it.u, false)
		case <-time.After(s.t.tick):
			s.mu.Lock()
			on, freq := s.on, s.freq
			s.mu.Unlock()
			if !on || freq == "" {
				continue
			}
			if airport, text, ok := s.atisOn(freq); ok {
				s.broadcast(airport, freq, text)
			}
		}
	}
}

// broadcast plays the ATIS on freq as a continuous broadcast: each
// information is synthesised once and loops from a fixed start, so tuning
// in joins it where it is, mid-sentence, as on a real receiver. It returns
// after one run, or when the frequency or the information changes.
func (s *Speaker) broadcast(airport, freq, text string) {
	s.mu.Lock()
	if !s.on || s.player == nil || s.eng == nil {
		s.mu.Unlock()
		return
	}
	e, p := s.eng, s.player
	cached := s.atisText == text && s.atisPCM != nil
	s.mu.Unlock()
	if !cached {
		voice := e.pool.Assign(airport, voicegoio.ATIS)
		pcm, err := e.tts.Synthesize(context.Background(), voice, SpokenEnd(e.norm.Spoken(text, voicegoio.ICAO)))
		if err != nil {
			s.opt.Logf("voice: %v", err)
			return
		}
		rate := e.tts.SampleRate(voice)
		out := e.chain.Apply(Pad(pcm, rate), rate, voice.Radio, p.SampleRate(), 1)
		s.mu.Lock()
		s.atisText, s.atisPCM, s.atisStart = text, out, time.Now()
		s.mu.Unlock()
	}
	s.mu.Lock()
	out, start := s.atisPCM, s.atisStart
	s.mu.Unlock()
	rate := p.SampleRate()
	length := samplesDuration(len(out), rate)
	period := length + s.t.atisGap
	into := time.Since(start) % period
	if into >= length {
		// Between two runs: the next one from its start, unless the
		// frequency changes meanwhile.
		if !s.wait(period-into, freq, p) {
			return
		}
		into = 0
	}
	from := min(int(into.Seconds()*float64(rate)), len(out))
	if err := p.Play(voicegoio.Transmission{Frequency: QueueKey, ControllerID: "atis", Phraseology: voicegoio.ICAO, Text: text}, out[from:], rate); err != nil {
		return
	}
	// Wait while it plays, but stop at a change of frequency (Set closes
	// the player).
	s.wait(length-into, freq, p)
}

// wait waits d while the speaker stays on freq with p, and reports whether
// it did: a change of frequency ends it at once, so the new frequency is
// heard without the old one's delay.
func (s *Speaker) wait(d time.Duration, freq string, p player) bool {
	for end := time.Now().Add(d); time.Now().Before(end); {
		s.mu.Lock()
		same := s.freq == freq && s.player == p && s.on
		s.mu.Unlock()
		if !same {
			return false
		}
		time.Sleep(min(s.t.tick, time.Until(end)))
	}
	return true
}

// voiceOf is the voice u is said in: its own (Voice), the crew's, or the
// controller on shift at the position.
func (s *Speaker) voiceOf(e *engine, u Utterance) voicegoio.VoiceProfile {
	if u.Voice != nil {
		v := *u.Voice
		if v.Radio == "" {
			v.Radio = string(KindOf(u.Position))
			if u.Pilot {
				v.Radio = string(voicegoio.Center)
			}
		}
		return v
	}
	if u.Pilot {
		return e.pool.AssignCrew(u.Callsign) // each crew its own voice, never a controller's
	}
	if u.Controller != "" {
		// The person on shift, whatever frequency: picked once (as a tower
		// voice), heard with the radio sound of the position worked now.
		v := e.pool.Assign(s.onShift(u.Airport, "controller "+u.Controller)+" "+u.Controller, voicegoio.Tower)
		v.Radio = string(KindOf(u.Position))
		return v
	}
	return e.pool.Assign(s.onShift(u.Airport, u.Position), KindOf(u.Position))
}

// say synthesises u in its speaker's voice and waits while it is played;
// force says it with the speaker off (SayOnce).
func (s *Speaker) say(u Utterance, force bool) {
	s.mu.Lock()
	if !s.on && !force || s.player == nil || s.eng == nil {
		s.mu.Unlock()
		return
	}
	e, p, freq := s.eng, s.player, s.freq
	s.mu.Unlock()

	voice := s.voiceOf(e, u)
	ph := phraseology(u.Phraseology)
	pcm, err := e.tts.Synthesize(context.Background(), voice, SpokenEnd(e.norm.Spoken(u.Text, ph)))
	if err != nil {
		s.opt.Logf("voice: %v", err)
		return
	}
	rate := e.tts.SampleRate(voice)
	out := e.chain.Apply(Pad(pcm, rate), rate, voice.Radio, p.SampleRate(), int64(len(u.Text)))
	// The pause since the last transmission, synthesis included.
	s.mu.Lock()
	gap := s.t.gap
	if s.t.jitter > 0 {
		gap += time.Duration(s.rng.Int64N(int64(s.t.jitter)))
	}
	if len(s.queue) > 2 {
		gap /= 2 // behind: shorter pauses rather than dropping calls
	}
	wait := time.Until(s.lastEnd.Add(gap))
	s.mu.Unlock()
	if wait > 0 && !s.wait(wait, freq, p) {
		return // another frequency meanwhile
	}
	if s.opt.OnSay != nil {
		s.opt.OnSay(u)
	}
	if err := p.Play(voicegoio.Transmission{Frequency: QueueKey, ControllerID: who(u), Phraseology: ph, Text: u.Text}, out, p.SampleRate()); err != nil {
		return // turned off meanwhile
	}
	// Wait while it is said, so the queue stays on the lag it has.
	said := samplesDuration(len(out), p.SampleRate())
	s.mu.Lock()
	s.lastEnd, s.lastCS = time.Now().Add(said), u.Callsign
	s.mu.Unlock()
	s.wait(said, freq, p)
}

// Clip is u as said on the radio, at the voice's own rate, without a
// player: for a client that plays the radio on its own device. ATIS
// utterances (PosATIS) are in the airport's ATIS voice unless u.Voice says
// otherwise; Intercom utterances come without the radio chain.
func (s *Speaker) Clip(u Utterance) (pcm []int16, rate int, err error) {
	if u.Chime != "" {
		if pcm := ChimePCM(u.Chime); pcm != nil {
			return pcm, ChimeRate, nil
		}
		return nil, 0, fmt.Errorf("speaker: unknown chime %q", u.Chime)
	}
	s.mu.Lock()
	err = s.openEngineLocked()
	e := s.eng
	s.mu.Unlock()
	if err != nil {
		return nil, 0, err
	}
	var voice voicegoio.VoiceProfile
	if u.Position == PosATIS && !u.Pilot && u.Voice == nil {
		voice = e.pool.Assign(u.Airport, voicegoio.ATIS)
	} else {
		voice = s.voiceOf(e, u)
	}
	raw, err := e.tts.Synthesize(context.Background(), voice, e.norm.Spoken(u.Text, phraseology(u.Phraseology)))
	if err != nil {
		return nil, 0, err
	}
	rate = e.tts.SampleRate(voice)
	if u.PA {
		return PAChain(raw, rate), rate, nil
	}
	if u.Intercom {
		return raw, rate, nil
	}
	return e.chain.Apply(raw, rate, voice.Radio, rate, int64(len(u.Text))), rate, nil
}

// WAV is pcm (16-bit mono at rate) as a WAV file.
func WAV(pcm []int16, rate int) []byte {
	var b bytes.Buffer
	_ = wav.Write(&b, pcm, rate) // a bytes.Buffer does not fail
	return b.Bytes()
}

// onShift is the voice key of the controller working pos at airport now:
// the airport, with the shift number once the first has handed over
// ("LKPR", then "LKPR-2"): the airport's prefix still picks the voices of
// its region.
func (s *Speaker) onShift(airport, pos string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shifts == nil {
		s.shifts = map[string]*shift{}
	}
	k := airport + "/" + pos
	sh := s.shifts[k]
	now := time.Now()
	length := func() time.Duration { return ShiftMin + time.Duration(s.rng.Int64N(int64(ShiftMax-ShiftMin))) }
	if sh == nil {
		sh = &shift{n: 1, until: now.Add(length())}
		s.shifts[k] = sh
	}
	for now.After(sh.until) {
		sh.n++
		sh.until = sh.until.Add(length())
	}
	if sh.n == 1 {
		return airport
	}
	return fmt.Sprintf("%s-%d", airport, sh.n)
}

// KindOf is the controller kind of a position: its voice and radio sound.
func KindOf(position string) voicegoio.ControllerKind {
	switch position {
	case PosDelivery, PosGround:
		return voicegoio.Ground
	case PosApproach, PosDeparture:
		return voicegoio.Approach
	case PosCenter:
		return voicegoio.Center
	case PosATIS:
		return voicegoio.ATIS
	default:
		return voicegoio.Tower
	}
}

func phraseology(p voicegoio.Phraseology) voicegoio.Phraseology {
	if p == voicegoio.FAA {
		return voicegoio.FAA
	}
	return voicegoio.ICAO
}

// SpokenEnd ends text with a full stop: piper cuts the last syllable of a
// sentence left open, and a call ends on a call sign ("…, Wizzair 1387").
func SpokenEnd(text string) string {
	text = strings.TrimRight(text, " ")
	if text == "" || strings.ContainsAny(text[len(text)-1:], ".?!") {
		return text
	}
	return text + "."
}

// Pad adds TailPad of silence after a synthesised call at rate.
func Pad(pcm []int16, rate int) []int16 {
	return append(pcm, make([]int16, int(TailPad.Seconds()*float64(rate)))...)
}

func samplesDuration(n, rate int) time.Duration {
	return time.Duration(float64(n) / float64(rate) * float64(time.Second))
}

// playChime plays chime u on the intercom player p from next on; after a
// call, the next item waits for the one called to pick up.
func (s *Speaker) playChime(l *lane, u Utterance, p player, next time.Time) {
	pcm := ChimePCM(u.Chime)
	if !s.sleep(time.Until(next)) {
		return
	}
	if s.opt.OnSay != nil {
		s.opt.OnSay(u)
	}
	if err := p.Play(voicegoio.Transmission{Frequency: l.key, ControllerID: "chime", Text: string(u.Chime)}, pcm, ChimeRate); err != nil {
		return
	}
	said := samplesDuration(len(pcm), ChimeRate)
	if u.Chime == ChimeCall {
		s.mu.Lock()
		said += s.t.pickup
		if s.t.pickupJitter > 0 {
			said += time.Duration(s.rng.Int64N(int64(s.t.pickupJitter)))
		}
		s.mu.Unlock()
	}
	s.mu.Lock()
	l.lastEnd = time.Now().Add(said - s.t.icGap)
	l.chimeEnd = l.lastEnd
	s.mu.Unlock()
	s.sleep(said - s.t.icGap)
}

// voiceKey is a voice as one person: its model and speaker.
func voiceKey(v voicegoio.VoiceProfile) string {
	return v.Model + "#" + strconv.Itoa(v.SpeakerID)
}
