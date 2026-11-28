// Package whisper is the cross-platform speech recognition backend: a
// whisper.cpp binary (whisper-cli) run as a subprocess on each push to talk
// cycle, as tts/piper runs piper. It is offline, needs no cgo, and is the
// engine on macOS and Linux; on Windows SAPI stays the default.
//
// whisper transcribes free text, so the grammar is applied after it: the
// transcript is spelt out as spoken ATC (Spoken) and tagged by the same
// parser stt/fake uses for the pilot grammar (atc.grxml) or, with
// Options.Controller, the controller grammar (controller.grxml). A
// Recognition is therefore the same {intent, callsign, value} SAPI gives.
// The aircraft on frequency go into whisper's initial prompt, which biases
// it towards their callsigns.
//
// Install fetches the binary (whisper.cpp's own Windows and Linux builds,
// our macOS builds; on macOS a Homebrew whisper-cli is used when it is
// there) and InstallModel a ggml model.
package whisper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/normalise"
	"github.com/mrlm-net/voice-goio/stt/fake"
)

// ErrNoRecorder is returned by Start when the platform has no default
// recorder and Options.Recorder is nil.
var ErrNoRecorder = errors.New("whisper: no recorder for this platform; set Options.Recorder")

// Options configures the recogniser.
type Options struct {
	// Bin is the whisper-cli binary. Empty: Find(dir of Model).
	Bin string
	// Model is a ggml model file (InstallModel). Required.
	Model string
	// Controller tags controller instructions (grammar.Controller) instead
	// of pilot transmissions.
	Controller bool
	// Prompt is put before the callsigns in whisper's initial prompt; ""
	// a default with the phraseology's words.
	Prompt string
	// Threads for whisper; 0: half the CPUs, at least 1.
	Threads int
	// Recorder captures the microphone between Start and Stop; nil the
	// platform's (Linux arecord, macOS sox), none on Windows.
	Recorder Recorder
	// MinConfidence below which a result is reported as say_again; 0: 0.5.
	MinConfidence float32
	// Timeout for one transcription; 0: 20 s.
	Timeout time.Duration
}

// Recognizer is a voicegoio.STT on whisper.cpp.
type Recognizer struct {
	opt    Options
	parser *fake.Parser

	mu        sync.Mutex
	callsigns []voicegoio.Callsign
	recording bool
	closed    bool
	results   chan voicegoio.Recognition
	wg        sync.WaitGroup
}

var _ voicegoio.STT = (*Recognizer)(nil)

// New checks the binary and the model and returns a recogniser.
func New(opt Options) (*Recognizer, error) {
	if opt.Model == "" {
		return nil, errors.New("whisper: no model")
	}
	if _, err := os.Stat(opt.Model); err != nil {
		return nil, fmt.Errorf("whisper: model: %w", err)
	}
	if opt.Bin == "" {
		bin, err := Find(filepath.Dir(opt.Model))
		if err != nil {
			return nil, err
		}
		opt.Bin = bin
	}
	if opt.Recorder == nil {
		opt.Recorder = defaultRecorder()
	}
	if opt.MinConfidence == 0 {
		opt.MinConfidence = 0.5
	}
	if opt.Timeout == 0 {
		opt.Timeout = 20 * time.Second
	}
	if opt.Threads == 0 {
		opt.Threads = max(1, runtime.NumCPU()/2)
	}
	p := fake.NewParser()
	if opt.Controller {
		p = fake.NewControllerParser()
	}
	return &Recognizer{opt: opt, parser: p, results: make(chan voicegoio.Recognition, 4)}, nil
}

// SetCallsigns sets the aircraft on frequency: the parser's roster and
// whisper's prompt.
func (r *Recognizer) SetCallsigns(cs []voicegoio.Callsign) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callsigns = append([]voicegoio.Callsign(nil), cs...)
	r.parser.SetCallsigns(cs)
	return nil
}

// Start begins recording (PTT pressed).
func (r *Recognizer) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("whisper: closed")
	}
	if r.recording {
		return nil
	}
	if r.opt.Recorder == nil {
		return ErrNoRecorder
	}
	if err := r.opt.Recorder.Start(); err != nil {
		return err
	}
	r.recording = true
	return nil
}

// Stop ends recording (PTT released); the recognition follows on Results.
func (r *Recognizer) Stop() error {
	r.mu.Lock()
	if !r.recording {
		r.mu.Unlock()
		return nil
	}
	r.recording = false
	r.wg.Add(1)
	r.mu.Unlock()
	path, err := r.opt.Recorder.Stop()
	go func() {
		defer r.wg.Done()
		if err != nil {
			r.results <- sayAgain("", voicegoio.ReasonOffGrammar)
			return
		}
		defer os.Remove(path)
		r.results <- r.RecognizeFile(path)
	}()
	return err
}

// Results delivers one Recognition per Stop.
func (r *Recognizer) Results() <-chan voicegoio.Recognition { return r.results }

// Close waits for a transcription under way and closes Results.
func (r *Recognizer) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	rec := r.recording
	r.recording = false
	r.mu.Unlock()
	if rec {
		if path, err := r.opt.Recorder.Stop(); err == nil {
			os.Remove(path)
		}
	}
	r.wg.Wait()
	close(r.results)
	return nil
}

// RecognizeFile transcribes a 16 kHz mono WAV and tags it: what Stop does
// with the recording, for corpora and tests.
func (r *Recognizer) RecognizeFile(wav string) voicegoio.Recognition {
	text, err := r.Transcribe(wav)
	if err != nil || strings.TrimSpace(text) == "" {
		return sayAgain(text, voicegoio.ReasonOffGrammar)
	}
	r.mu.Lock()
	rec := r.parser.Parse(Spoken(Written(text, r.callsigns)))
	r.mu.Unlock()
	rec.Text = text
	if rec.Confidence < r.opt.MinConfidence && rec.Tags[voicegoio.TagIntent] != voicegoio.IntentSayAgain {
		return sayAgain(text, voicegoio.ReasonLowConfidence)
	}
	return rec
}

// Transcribe runs whisper-cli on a WAV and returns its text.
func (r *Recognizer) Transcribe(wav string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), r.opt.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.opt.Bin, r.args(wav)...)
	hideWindow(cmd)
	if runtime.GOOS == "linux" {
		// whisper.cpp's Linux build loads its .so files from beside it.
		lib := filepath.Dir(r.opt.Bin)
		if old := os.Getenv("LD_LIBRARY_PATH"); old != "" {
			lib += ":" + old
		}
		cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+lib)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("whisper: %v: %s", err, strings.TrimSpace(errb.String()))
	}
	return strings.Join(strings.Fields(out.String()), " "), nil
}

// args are whisper-cli's: English, no timestamps, nothing but the text.
func (r *Recognizer) args(wav string) []string {
	return []string{"-m", r.opt.Model, "-f", wav, "-l", "en", "-nt", "-np",
		"-t", fmt.Sprint(r.opt.Threads), "--prompt", r.prompt()}
}

// DefaultPrompt is the start of whisper's initial prompt: the words of
// the phraseology, so they are written as said.
const DefaultPrompt = "Radio telephony, ICAO phraseology. Alpha, Bravo, Charlie, Delta, Echo, Foxtrot, Golf, Hotel, " +
	"India, Juliett, Kilo, Lima, Mike, November, Oscar, Papa, Quebec, Romeo, Sierra, Tango, Uniform, Victor, " +
	"Whiskey, X-ray, Yankee, Zulu. Niner, tree, fife, decimal. Flight level, squawk, QNH, runway, holding point, " +
	"line up and wait, cleared for take-off, cleared to land, wind, knots, pushback, start-up, taxi via, hold short, " +
	"cross runway, contact tower, go around, vacate, ident, wilco, roger, say again."

func (r *Recognizer) prompt() string {
	p := r.opt.Prompt
	if p == "" {
		p = DefaultPrompt
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var names []string
	for _, c := range r.callsigns {
		s := c.Spoken
		if s == "" {
			s = normalise.SpokenCallsign(c.ICAO)
		}
		names = append(names, s)
	}
	if len(names) > 0 {
		p += " Callsigns: " + strings.Join(names, ", ") + "."
	}
	return p
}

func sayAgain(text, reason string) voicegoio.Recognition {
	return voicegoio.Recognition{Text: text, Tags: map[string]string{
		voicegoio.TagIntent: voicegoio.IntentSayAgain, voicegoio.TagReason: reason}}
}
