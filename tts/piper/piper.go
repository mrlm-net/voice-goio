// Package piper drives the piper neural TTS binary as a sidecar process.
//
// piper is a self contained executable: no Go bindings, no cgo, no network.
// The library starts one long lived process per model file and speaks to it
// over JSON lines on stdin, reading raw 16 bit PCM back on stdout. Keeping the
// process warm is what makes the sub 300 ms first sample budget achievable;
// starting piper per utterance costs a model load every time.
//
// One process per model, not per speaker: a multi speaker model such as
// en_GB-vctk-medium carries 109 voices behind one set of weights, and
// --json-input lets speaker_id change per line.
package piper

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/internal/userdir"
)

// Defaults for Options.
const (
	DefaultPoolSize   = 4
	DefaultTimeout    = 5 * time.Second
	DefaultIdleGap    = 150 * time.Millisecond
	DefaultSampleRate = 22050
	sentinelText      = "dot."
	warmupRetries     = 1
)

// Resolver maps a model name to the files piper needs. The voices package
// implements it from voices.json, including the espeak override copies used by
// the non English swap trick; the default implementation just looks in a
// directory tree.
type Resolver interface {
	Resolve(model string) (onnx, config string, err error)
}

// Options configures the sidecar pool.
type Options struct {
	// PiperPath is the piper executable. Empty means bin/piper/piper[.exe]
	// next to the running executable.
	PiperPath string
	// VoicesDir is the root the default resolver searches. Empty means the
	// per user data directory (see DefaultVoicesDir).
	VoicesDir string
	// Resolver overrides model file lookup.
	Resolver Resolver
	// PoolSize is the number of concurrently warm processes; least recently
	// used models are evicted beyond it. 0 means DefaultPoolSize.
	PoolSize int
	// Timeout bounds one synthesis. 0 means DefaultTimeout.
	Timeout time.Duration
	// IdleGap is how long stdout must be quiet before an utterance is
	// considered finished. 0 means DefaultIdleGap.
	IdleGap time.Duration
	// Args appends extra flags to every piper invocation.
	Args []string
	// Stderr receives the sidecar's log output; nil discards it.
	Stderr io.Writer
}

// TTS is a pool of piper processes. It implements voicegoio.TTS and is safe for
// concurrent use.
type TTS struct {
	opt Options

	mu     sync.Mutex
	procs  map[string]*proc // by model name
	order  []string         // LRU, most recent last
	rates  map[string]int
	closed bool
}

// New prepares the pool. It does not start a process: the first Synthesize for
// a model starts it, so constructing a TTS is cheap and cannot fail on a
// machine where voices have not been downloaded yet.
func New(opt Options) (*TTS, error) {
	if opt.PoolSize <= 0 {
		opt.PoolSize = DefaultPoolSize
	}
	if opt.Timeout <= 0 {
		opt.Timeout = DefaultTimeout
	}
	if opt.IdleGap <= 0 {
		opt.IdleGap = DefaultIdleGap
	}
	if opt.PiperPath == "" {
		opt.PiperPath = DefaultPiperPath()
	}
	if opt.VoicesDir == "" {
		opt.VoicesDir = DefaultVoicesDir()
	}
	if opt.Resolver == nil {
		opt.Resolver = DirResolver(opt.VoicesDir)
	}
	return &TTS{opt: opt, procs: map[string]*proc{}, rates: map[string]int{}}, nil
}

// DefaultPiperPath is bin/piper/piper[.exe] next to the running executable,
// which is how the application ships it.
func DefaultPiperPath() string {
	name := "piper"
	if runtime.GOOS == "windows" {
		name = "piper.exe"
	}
	exe, err := os.Executable()
	if err != nil {
		return filepath.Join("bin", "piper", name)
	}
	return filepath.Join(filepath.Dir(exe), "bin", "piper", name)
}

// DefaultVoicesDir is the per user data directory voices are downloaded into.
func DefaultVoicesDir() string { return userdir.Voices() }

// DirResolver finds <model>.onnx and its .json anywhere under root, which is
// the layout HuggingFace's piper-voices repository uses
// (en/en_GB/vctk/medium/en_GB-vctk-medium.onnx).
type DirResolver string

func (d DirResolver) Resolve(model string) (string, string, error) {
	root := string(d)
	want := model + ".onnx"
	var found string
	err := filepath.WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return nil //nolint:nilerr // an unreadable subtree is not fatal
		}
		if e.Name() == want {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return "", "", fmt.Errorf("piper: search %s: %w", root, err)
	}
	if found == "" {
		return "", "", fmt.Errorf("piper: model %q not found under %s (run the voices downloader first)", model, root)
	}
	cfg := found + ".json"
	if _, err := os.Stat(cfg); err != nil {
		return "", "", fmt.Errorf("piper: config %s missing for model %q", cfg, model)
	}
	return found, cfg, nil
}

// modelConfig is the subset of the .onnx.json we read.
type modelConfig struct {
	Audio struct {
		SampleRate int `json:"sample_rate"`
	} `json:"audio"`
	NumSpeakers int `json:"num_speakers"`
}

// SampleRate reports the native rate of the model behind profile, read from its
// .onnx.json and cached. Unknown models report DefaultSampleRate, the rate of
// every "medium" voice.
func (t *TTS) SampleRate(p voicegoio.VoiceProfile) int {
	t.mu.Lock()
	if r, ok := t.rates[p.Model]; ok {
		t.mu.Unlock()
		return r
	}
	t.mu.Unlock()

	rate := DefaultSampleRate
	if _, cfg, err := t.opt.Resolver.Resolve(p.Model); err == nil {
		if b, err := os.ReadFile(cfg); err == nil {
			var mc modelConfig
			if json.Unmarshal(b, &mc) == nil && mc.Audio.SampleRate > 0 {
				rate = mc.Audio.SampleRate
			}
		}
	}
	t.mu.Lock()
	t.rates[p.Model] = rate
	t.mu.Unlock()
	return rate
}

// Synthesize speaks one already normalised transmission.
func (t *TTS) Synthesize(ctx context.Context, p voicegoio.VoiceProfile, text string) ([]int16, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	pr, err := t.acquire(p.Model)
	if err != nil {
		return nil, err
	}
	pcm, err := pr.speak(ctx, p, text, t.opt.Timeout, t.opt.IdleGap)
	if err == nil {
		return pcm, nil
	}
	// SPEC 4.2: on failure kill, restart, retry once. A sidecar that has wedged
	// on one malformed line stays wedged, so recycling is the only cure.
	t.drop(p.Model, pr)
	if ctx.Err() != nil {
		return nil, err
	}
	pr2, err2 := t.acquire(p.Model)
	if err2 != nil {
		return nil, errors.Join(err, err2)
	}
	pcm, err2 = pr2.speak(ctx, p, text, t.opt.Timeout, t.opt.IdleGap)
	if err2 != nil {
		return nil, fmt.Errorf("piper: %q failed twice: %w", p.Model, errors.Join(err, err2))
	}
	return pcm, nil
}

func (t *TTS) acquire(model string) (*proc, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, voicegoio.ErrClosed
	}
	if p, ok := t.procs[model]; ok && p.alive() {
		t.touch(model)
		return p, nil
	}
	onnx, cfg, err := t.opt.Resolver.Resolve(model)
	if err != nil {
		return nil, err
	}
	p, err := start(t.opt, onnx, cfg)
	if err != nil {
		return nil, err
	}
	t.procs[model] = p
	t.touch(model)
	for len(t.order) > t.opt.PoolSize {
		evict := t.order[0]
		t.order = t.order[1:]
		if old := t.procs[evict]; old != nil {
			old.stop()
			delete(t.procs, evict)
		}
	}
	return p, nil
}

func (t *TTS) touch(model string) {
	for i, m := range t.order {
		if m == model {
			t.order = append(t.order[:i], t.order[i+1:]...)
			break
		}
	}
	t.order = append(t.order, model)
}

func (t *TTS) drop(model string, p *proc) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.procs[model] == p {
		delete(t.procs, model)
		for i, m := range t.order {
			if m == model {
				t.order = append(t.order[:i], t.order[i+1:]...)
				break
			}
		}
	}
	p.stop()
}

// Close stops every sidecar.
func (t *TTS) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	for _, p := range t.procs {
		p.stop()
	}
	t.procs, t.order = nil, nil
	return nil
}

// Available reports whether the piper binary is present, so an application can
// fall back to another backend instead of failing the first transmission.
func (t *TTS) Available() error {
	if _, err := os.Stat(t.opt.PiperPath); err != nil {
		return fmt.Errorf("piper: executable %s not found: %w", t.opt.PiperPath, err)
	}
	return nil
}

// checkFlags reports which per line prosody keys the shipped binary accepts,
// by reading its --help. SPEC 4.2 requires this to be verified rather than
// assumed: older builds only take length_scale on the command line, in which
// case a profile's prosody must select a separate process instead of a field.
func CheckFlags(piperPath string) (perLineProsody bool, help string, err error) {
	// Bounded, because a piper that cannot start does not fail — it hangs.
	// The macOS build of 2023.11.14-2 does exactly that on Apple Silicon, and
	// an unbounded CombinedOutput here would wedge the caller forever rather
	// than report a broken install.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, piperPath, "--help")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return false, string(out), fmt.Errorf("piper: %s did not respond to --help within 20s; "+
			"the binary is probably the wrong architecture for this machine", piperPath)
	}
	help = string(out)
	if err != nil && help == "" {
		return false, "", fmt.Errorf("piper: --help: %w", err)
	}
	h := strings.ToLower(help)
	return strings.Contains(h, "length_scale") || strings.Contains(h, "length-scale"), help, nil
}

var _ voicegoio.TTS = (*TTS)(nil)

// bufferedStderr keeps the last few log lines so an error can quote the
// sidecar's own complaint rather than just "exit status 1".
type bufferedStderr struct {
	mu    sync.Mutex
	lines []string
	out   io.Writer
}

func (b *bufferedStderr) pump(r io.Reader) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		b.mu.Lock()
		b.lines = append(b.lines, sc.Text())
		if len(b.lines) > 20 {
			b.lines = b.lines[len(b.lines)-20:]
		}
		w := b.out
		b.mu.Unlock()
		if w != nil {
			fmt.Fprintln(w, sc.Text())
		}
	}
}

func (b *bufferedStderr) tail() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Join(b.lines, "\n")
}
