// Package audio owns the output device and the per frequency playback queues.
//
// The platform specific part is deliberately tiny: a sink that can enumerate
// devices, open one at a sample rate, and block until a buffer has finished
// playing. Everything above that (queueing, event reporting, rate conversion)
// is shared Go code, so the Windows backend only has to get waveOut right.
package audio

import (
	"fmt"
	"sync"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/internal/dsp"
	"github.com/mrlm-net/voice-goio/internal/wav"
)

// DefaultSampleRate is the rate the device is opened at. 48 kHz is what every
// Windows endpoint runs internally, so opening at 48 kHz avoids a second
// resample inside the audio engine.
const DefaultSampleRate = 48000

// DefaultDevice selects the system default output ("wave mapper" on Windows).
const DefaultDevice = ""

// sink is the platform contract. One implementation per GOOS, selected by build
// tag; newSink is declared in out_windows.go / out_darwin.go / out_other.go.
type sink interface {
	devices() ([]voicegoio.Device, error)
	open(deviceID string, sampleRate int) error
	// write blocks until the samples have been played.
	write(pcm []int16) error
	close() error
	// selectable reports whether this platform can route to a chosen device.
	selectable() bool
}

// Options configures a Player.
type Options struct {
	// DeviceID selects the output endpoint, as reported by Devices. Empty is
	// the system default.
	DeviceID string
	// SampleRate is the rate the device is opened at; 0 means DefaultSampleRate.
	SampleRate int
	// QueueDepth is the number of transmissions buffered per frequency before
	// Play reports the frequency as congested. 0 means 16.
	QueueDepth int
	// WAVDir writes each transmission to a WAV file in this directory instead
	// of playing it, on any platform. Empty uses the platform's audio output,
	// unless VOICEGOIO_OUT_DIR is set.
	WAVDir string
	// RecordPath records everything that goes to the device into one
	// continuous WAV file, as well as playing it. This is a session recording:
	// the whole frequency as it was heard, in one file that can be sent to
	// somebody.
	RecordPath string
	// RecordGapMS is the silence inserted between transmissions in the
	// recording, so they do not run into each other. 0 means 500.
	RecordGapMS int
	// Silent accepts audio and plays nothing. Recording still happens, so this
	// is how a render produces a file without also shouting through the
	// speakers for several minutes.
	Silent bool
}

type item struct {
	t    voicegoio.Transmission
	pcm  []int16
	rate int
}

// Player implements voicegoio.Player.
//
// Each frequency gets its own queue and goroutine, so a busy tower frequency
// never delays a clearance on ground. Access to the device itself is
// serialised: one transmission is audible at a time, which is also what a real
// receiver does.
type Player struct {
	mu      sync.Mutex
	sink    sink
	rate    int
	depth   int
	queues  map[string]chan item
	rec     *wav.Writer
	recPath string
	recGap  int // samples of silence between transmissions
	events  chan voicegoio.PlaybackEvent
	wg      sync.WaitGroup
	done    chan struct{}
	closed  bool
}

// NewPlayer opens the output device.
func NewPlayer(opts Options) (*Player, error) {
	rate := opts.SampleRate
	if rate == 0 {
		rate = DefaultSampleRate
	}
	depth := opts.QueueDepth
	if depth == 0 {
		depth = 16
	}
	var s sink
	var err error
	switch {
	case opts.Silent:
		s = &silentSink{}
	case opts.WAVDir != "":
		s = &fileSink{dir: opts.WAVDir}
	default:
		if s, err = newSink(); err != nil {
			return nil, err
		}
	}
	if err := s.open(opts.DeviceID, rate); err != nil {
		return nil, err
	}
	p := &Player{
		sink:   s,
		rate:   rate,
		depth:  depth,
		queues: make(map[string]chan item),
		events: make(chan voicegoio.PlaybackEvent, 128),
		done:   make(chan struct{}),
	}
	if opts.RecordPath != "" {
		w, err := wav.Create(opts.RecordPath, rate)
		if err != nil {
			s.close()
			return nil, fmt.Errorf("audio: record to %s: %w", opts.RecordPath, err)
		}
		gap := opts.RecordGapMS
		if gap == 0 {
			gap = 500
		}
		p.rec, p.recPath, p.recGap = w, opts.RecordPath, gap*rate/1000
	}
	return p, nil
}

// Recording reports the path and duration of the session recording, if one is
// being made.
func (p *Player) Recording() (path string, seconds float64, ok bool) {
	if p.rec == nil {
		return "", 0, false
	}
	return p.recPath, p.rec.Duration(), true
}

// SampleRate reports the rate the device is open at.
func (p *Player) SampleRate() int { return p.rate }

// Devices enumerates the output endpoints.
func (p *Player) Devices() ([]voicegoio.Device, error) { return p.sink.devices() }

// SetDevice reopens the output on another endpoint. In flight audio is
// finished on the old device first.
//
// On platforms without device selection (macOS, where playback shells out to
// afplay) this is a documented no-op that returns nil, so settings UI code does
// not need a platform switch.
func (p *Player) SetDevice(id string) error {
	if !p.sink.selectable() {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return voicegoio.ErrClosed
	}
	if err := p.sink.close(); err != nil {
		return err
	}
	return p.sink.open(id, p.rate)
}

// Play queues one transmission behind anything already queued for its
// frequency. It does not block for the duration of the audio.
func (p *Player) Play(t voicegoio.Transmission, pcm []int16, sampleRate int) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return voicegoio.ErrClosed
	}
	q, ok := p.queues[t.Frequency]
	if !ok {
		q = make(chan item, p.depth)
		p.queues[t.Frequency] = q
		p.wg.Add(1)
		go p.serve(q)
	}
	p.mu.Unlock()

	if sampleRate == 0 {
		sampleRate = p.rate
	}
	select {
	case q <- item{t: t, pcm: pcm, rate: sampleRate}:
		return nil
	default:
		return fmt.Errorf("audio: frequency %s is congested (%d queued)", t.Frequency, p.depth)
	}
}

// serve drains one frequency's queue.
func (p *Player) serve(q chan item) {
	defer p.wg.Done()
	for {
		select {
		case <-p.done:
			return
		case it, ok := <-q:
			if !ok {
				return
			}
			p.emit(voicegoio.PlaybackEvent{Frequency: it.t.Frequency, ControllerID: it.t.ControllerID, Started: true})
			pcm := it.pcm
			if it.rate != p.rate {
				pcm = dsp.ToInt16(dsp.Resample(dsp.FromInt16(pcm), it.rate, p.rate))
			}
			p.mu.Lock()
			s, closed, rec := p.sink, p.closed, p.rec
			p.mu.Unlock()
			if !closed {
				// Record before playing, so the recording holds exactly what
				// the device was given, in the order it was given it.
				if rec != nil {
					_ = rec.Append(pcm)
					_ = rec.AppendSilence(p.recGap)
				}
				// The device write is serialised by the sink itself; only one
				// frequency is audible at a time.
				_ = s.write(pcm)
			}
			p.emit(voicegoio.PlaybackEvent{Frequency: it.t.Frequency, ControllerID: it.t.ControllerID, Started: false})
		}
	}
}

// emit delivers a playback event. The channel is buffered; if the application
// is not draining Events, playback blocks rather than silently losing the
// Finished event the application uses to release the frequency.
func (p *Player) emit(ev voicegoio.PlaybackEvent) {
	select {
	case p.events <- ev:
	case <-p.done:
	}
}

// Events reports Started/Finished per transmission. It must be drained.
func (p *Player) Events() <-chan voicegoio.PlaybackEvent { return p.events }

// Close stops playback and releases the device.
func (p *Player) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.done)
	for _, q := range p.queues {
		close(q)
	}
	s := p.sink
	p.mu.Unlock()

	p.wg.Wait()
	close(p.events)
	if p.rec != nil {
		if err := p.rec.Close(); err != nil {
			s.close()
			return err
		}
	}
	return s.close()
}

var _ voicegoio.Player = (*Player)(nil)
