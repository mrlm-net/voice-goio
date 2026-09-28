package fake

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	voicegoio "github.com/mrlm-net/voice-goio"
)

// ScriptLine is one entry of a recognition script (testdata/stt/*.jsonl).
//
// A line either sets the roster or carries a transmission. Expected tags are
// optional: when absent the parser's own output is used, which makes the file
// usable both as a corpus with assertions and as a plain list of phrases.
type ScriptLine struct {
	Callsigns  []string          `json:"callsigns,omitempty"`
	Text       string            `json:"text,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
	Confidence float32           `json:"confidence,omitempty"`
}

// Recognizer is the non Windows implementation of voicegoio.STT.
//
// Two sources are supported. A reader (typically stdin) treats every line typed
// as one complete push to talk cycle, which is how the pipeline is driven
// during development on macOS. A script pops one line per Stop, which is how
// the regression corpus is replayed.
type Recognizer struct {
	parser *Parser

	mu      sync.Mutex
	script  []ScriptLine
	next    int
	started bool
	closed  bool

	results chan voicegoio.Recognition
	done    chan struct{}
	wg      sync.WaitGroup
}

// FromStdin reads transmissions from standard input.
func FromStdin() *Recognizer { return FromReader(os.Stdin) }

// FromReader reads one transmission per line. Start and Stop are accepted but
// not required: the line itself is the push to talk cycle.
//
// When the reader reaches EOF the results channel is closed, so a consumer
// ranging over Results, or checking the second return value of a receive,
// learns that there will be no more transmissions instead of blocking forever.
func FromReader(r io.Reader) *Recognizer {
	rec := newRecognizer()
	rec.wg.Add(1)
	go func() {
		defer rec.wg.Done()
		// Only this goroutine ever sends on results for a reader backed
		// recogniser, so it is the one that may close the channel.
		defer close(rec.results)
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			rec.mu.Lock()
			p := rec.parser
			rec.mu.Unlock()
			select {
			case rec.results <- p.Parse(line):
			case <-rec.done:
				return
			}
		}
	}()
	return rec
}

// FromScript loads a JSON lines corpus.
func FromScript(path string) (*Recognizer, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	lines, err := ReadScript(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return FromLines(lines), nil
}

// ReadScript parses a JSON lines corpus, skipping blank lines and # comments.
func ReadScript(r io.Reader) ([]ScriptLine, error) {
	var out []ScriptLine
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var sl ScriptLine
		if err := json.Unmarshal([]byte(line), &sl); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		out = append(out, sl)
	}
	return out, sc.Err()
}

// FromLines replays an in-memory script.
func FromLines(lines []ScriptLine) *Recognizer {
	rec := newRecognizer()
	rec.script = lines
	// A leading roster directive applies before the first Stop.
	if len(lines) > 0 && len(lines[0].Callsigns) > 0 {
		rec.applyRoster(lines[0].Callsigns)
		rec.next = 1
	}
	return rec
}

func newRecognizer() *Recognizer {
	return &Recognizer{
		parser:  NewParser(),
		results: make(chan voicegoio.Recognition, 8),
		done:    make(chan struct{}),
	}
}

func (r *Recognizer) applyRoster(ids []string) {
	cs := make([]voicegoio.Callsign, 0, len(ids))
	for _, id := range ids {
		cs = append(cs, voicegoio.Callsign{ICAO: id})
	}
	r.parser.SetCallsigns(cs)
}

// SetCallsigns restricts recognition to the aircraft on frequency.
func (r *Recognizer) SetCallsigns(cs []voicegoio.Callsign) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return voicegoio.ErrClosed
	}
	r.parser.SetCallsigns(cs)
	return nil
}

// Parser exposes the tag parser, so cmd/voicecheck can tag a phrase without
// driving a recogniser.
func (r *Recognizer) Parser() *Parser { return r.parser }

// Start marks the beginning of a push to talk cycle.
func (r *Recognizer) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return voicegoio.ErrClosed
	}
	r.started = true
	return nil
}

// Stop ends the cycle. For a script backed recogniser it emits the next line;
// for a reader backed one the result has already been emitted by the reader.
func (r *Recognizer) Stop() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return voicegoio.ErrClosed
	}
	r.started = false
	if r.script == nil {
		r.mu.Unlock()
		return nil
	}
	// Skip any roster directives, applying them as we go.
	for r.next < len(r.script) && len(r.script[r.next].Callsigns) > 0 {
		r.applyRoster(r.script[r.next].Callsigns)
		r.next++
	}
	if r.next >= len(r.script) {
		r.mu.Unlock()
		// Out of script: the engine heard nothing usable.
		return r.emit(voicegoio.Recognition{
			Tags: map[string]string{voicegoio.TagIntent: voicegoio.IntentSayAgain},
		})
	}
	line := r.script[r.next]
	r.next++
	rec := r.parser.Parse(line.Text)
	if len(line.Tags) > 0 {
		rec.Tags = line.Tags
	}
	if line.Confidence > 0 {
		rec.Confidence = line.Confidence
	}
	r.mu.Unlock()
	return r.emit(rec)
}

func (r *Recognizer) emit(rec voicegoio.Recognition) error {
	select {
	case r.results <- rec:
		return nil
	case <-r.done:
		return voicegoio.ErrClosed
	}
}

// Results delivers one recognition per push to talk cycle.
func (r *Recognizer) Results() <-chan voicegoio.Recognition { return r.results }

// Remaining reports how many script lines are left, for regression runners.
func (r *Recognizer) Remaining() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return max(len(r.script)-r.next, 0)
}

// Close stops the reader goroutine and releases the results channel.
func (r *Recognizer) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	close(r.done)
	r.mu.Unlock()
	// The reader goroutine may be blocked on an unbuffered stdin read; it exits
	// when that read returns, and cannot publish because done is closed.
	return nil
}

var _ voicegoio.STT = (*Recognizer)(nil)
