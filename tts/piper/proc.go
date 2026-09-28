package piper

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/internal/jsonl"
)

// proc is one warm piper process bound to one model file.
//
// Utterance framing is the hard part of this backend. In --output-raw mode
// piper writes the samples for a line and then simply waits for the next one:
// there is no length prefix and no end marker. SPEC 4.2 resolves this with a
// sentinel: after the real text we send a fixed short word, so the stream ends
// with a known number of samples that we cut off. The sentinel also protects
// the idle detector, because a stall in the middle of a long sentence is
// followed by more audio, whereas the end of a transmission is followed only by
// the sentinel's tail.
type proc struct {
	cmd    *exec.Cmd
	in     *jsonl.Writer
	stdout io.ReadCloser
	errBuf *bufferedStderr

	chunks chan []byte
	readEr atomic.Pointer[error]
	dead   atomic.Bool

	mu        sync.Mutex     // one utterance at a time per process
	sentinels map[string]int // samples of sentinel audio per voice shape
}

func start(opt Options, onnx, cfg string) (*proc, error) {
	args := []string{
		"--model", onnx,
		"--config", cfg,
		"--output-raw",
		"--json-input",
		"--sentence_silence", "0",
	}
	args = append(args, opt.Args...)
	cmd := exec.Command(opt.PiperPath, args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("piper: stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("piper: stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("piper: stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("piper: start %s: %w", opt.PiperPath, err)
	}

	p := &proc{
		cmd:       cmd,
		in:        jsonl.NewWriter(stdin),
		stdout:    stdout,
		errBuf:    &bufferedStderr{out: opt.Stderr},
		chunks:    make(chan []byte, 64),
		sentinels: map[string]int{},
	}
	go p.errBuf.pump(stderr)
	go p.read()
	go func() {
		_ = cmd.Wait()
		p.dead.Store(true)
	}()
	return p, nil
}

func (p *proc) alive() bool { return !p.dead.Load() }

// read pumps stdout into chunks until the process exits.
func (p *proc) read() {
	defer close(p.chunks)
	buf := make([]byte, 16<<10)
	for {
		n, err := p.stdout.Read(buf)
		if n > 0 {
			b := make([]byte, n)
			copy(b, buf[:n])
			p.chunks <- b
		}
		if err != nil {
			if err != io.EOF {
				p.readEr.Store(&err)
			}
			return
		}
	}
}

// shape keys the sentinel length cache: prosody and speaker both change how
// long the sentinel word takes to say.
func shape(v voicegoio.VoiceProfile) string {
	return fmt.Sprintf("%d/%.2f/%.2f/%.2f", v.SpeakerID, v.LengthScale, v.NoiseScale, v.NoiseW)
}

func line(v voicegoio.VoiceProfile, text string) jsonl.Line {
	l := jsonl.Line{Text: text}
	if v.SpeakerID > 0 {
		l.SpeakerID = jsonl.Int(v.SpeakerID)
	}
	if v.LengthScale > 0 {
		l.LengthScale = jsonl.Float32(v.LengthScale)
	}
	if v.NoiseScale > 0 {
		l.NoiseScale = jsonl.Float32(v.NoiseScale)
	}
	if v.NoiseW > 0 {
		l.NoiseW = jsonl.Float32(v.NoiseW)
	}
	return l
}

// speak synthesises one utterance.
func (p *proc) speak(ctx context.Context, v voicegoio.VoiceProfile, text string, timeout, idleGap time.Duration) ([]int16, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.alive() {
		return nil, fmt.Errorf("piper: sidecar exited: %s", p.errBuf.tail())
	}
	p.drain()

	key := shape(v)
	sentinel, ok := p.sentinels[key]
	if !ok {
		// Measure the sentinel once per voice shape.
		raw, err := p.round(ctx, []jsonl.Line{line(v, sentinelText)}, timeout, idleGap)
		if err != nil {
			return nil, err
		}
		sentinel = len(raw)
		if sentinel == 0 {
			return nil, fmt.Errorf("piper: sidecar produced no audio for the warm up utterance: %s", p.errBuf.tail())
		}
		p.sentinels[key] = sentinel
	}

	raw, err := p.round(ctx, []jsonl.Line{line(v, text), line(v, sentinelText)}, timeout, idleGap)
	if err != nil {
		return nil, err
	}
	if len(raw) <= sentinel {
		// The sentinel never arrived: return what we have rather than nothing,
		// and let the caller hear a slightly long transmission.
		return raw, nil
	}
	out := raw[:len(raw)-sentinel]
	return trimSilence(out), nil
}

// round writes the given lines and collects PCM until stdout has been quiet for
// idleGap, the process dies, or the deadline passes.
func (p *proc) round(ctx context.Context, lines []jsonl.Line, timeout, idleGap time.Duration) ([]int16, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for _, l := range lines {
		if err := p.in.Encode(l); err != nil {
			return nil, fmt.Errorf("piper: write stdin: %w (%s)", err, p.errBuf.tail())
		}
	}

	var bytesBuf []byte
	idle := time.NewTimer(timeout) // the first chunk may take a model warm up
	defer idle.Stop()
	for {
		select {
		case <-ctx.Done():
			if len(bytesBuf) > 0 {
				return toPCM(bytesBuf), nil
			}
			return nil, fmt.Errorf("piper: timed out after %s waiting for audio (%s)", timeout, p.errBuf.tail())
		case b, ok := <-p.chunks:
			if !ok {
				if len(bytesBuf) > 0 {
					return toPCM(bytesBuf), nil
				}
				if e := p.readEr.Load(); e != nil {
					return nil, fmt.Errorf("piper: read stdout: %w (%s)", *e, p.errBuf.tail())
				}
				return nil, fmt.Errorf("piper: sidecar closed its output: %s", p.errBuf.tail())
			}
			bytesBuf = append(bytesBuf, b...)
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(idleGap)
		case <-idle.C:
			if len(bytesBuf) == 0 {
				return nil, fmt.Errorf("piper: no audio within %s: %s", timeout, p.errBuf.tail())
			}
			return toPCM(bytesBuf), nil
		}
	}
}

// drain discards audio left over from a previous, failed utterance so it cannot
// be prefixed onto the next transmission.
func (p *proc) drain() {
	for {
		select {
		case <-p.chunks:
		default:
			return
		}
	}
}

func toPCM(b []byte) []int16 {
	out := make([]int16, len(b)/2)
	for i := range out {
		out[i] = int16(uint16(b[i*2]) | uint16(b[i*2+1])<<8)
	}
	return out
}

// trimSilence removes a silent tail, which is what is left after the sentinel
// is cut and what --sentence_silence 0 does not quite eliminate.
func trimSilence(pcm []int16) []int16 {
	const threshold = 32 // about -60 dBFS
	end := len(pcm)
	for end > 0 {
		v := pcm[end-1]
		if v > threshold || v < -threshold {
			break
		}
		end--
	}
	return pcm[:end]
}

func (p *proc) stop() {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	p.dead.Store(true)
}
