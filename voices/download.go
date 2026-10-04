package voices

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the HuggingFace repository piper voices are published in.
// It is configurable so a user behind a slow link can point at a LAN mirror,
// and so the acceptance test with the network adapter disabled can point at a
// local directory served over HTTP.
const DefaultBaseURL = "https://huggingface.co/rhasspy/piper-voices/resolve/main/"

// Progress reports download progress for one file.
type Progress struct {
	Model      string
	File       string
	Downloaded int64
	Total      int64 // -1 when the server does not say
}

// Downloader fetches models into the pool directory.
//
// Voices are never bundled with the library: they are hundreds of megabytes,
// they have their own licences, and a user only needs the ones their region
// uses. The first run downloads them; every run after that is offline.
type Downloader struct {
	// BaseURL to fetch from. Empty means DefaultBaseURL.
	BaseURL string
	// Dir is the destination root. Empty means the pool's directory.
	Dir string
	// Client is the HTTP client. Nil means a client with a 30 minute timeout,
	// because a "medium" model over a domestic connection is not quick.
	Client *http.Client
	// OnProgress is called at most a few times a second per file.
	OnProgress func(Progress)
}

func (d *Downloader) client() *http.Client {
	if d.Client != nil {
		return d.Client
	}
	return &http.Client{Timeout: 30 * time.Minute}
}

func (d *Downloader) baseURL() string {
	if d.BaseURL == "" {
		return DefaultBaseURL
	}
	if !strings.HasSuffix(d.BaseURL, "/") {
		return d.BaseURL + "/"
	}
	return d.BaseURL
}

// Fetch downloads one model's .onnx and .onnx.json if they are not already
// present and valid.
//
// When the manifest carries a sha256 the file is verified against it and a
// mismatch is an error. When it does not, the computed hash is returned so
// cmd/voicecheck can write it back into the manifest: a hash recorded from a
// download you trust is better than no hash at all.
func (d *Downloader) Fetch(ctx context.Context, m *Model) (sha string, err error) {
	if err := d.fetchFile(ctx, m.Name, m.Config, ""); err != nil {
		return "", err
	}
	return d.fetchFileSum(ctx, m.Name, m.ONNX, m.SHA256)
}

func (d *Downloader) fetchFile(ctx context.Context, model, rel, want string) error {
	_, err := d.fetchFileSum(ctx, model, rel, want)
	return err
}

// fetchFileSum downloads one file with resume support and returns its hash.
func (d *Downloader) fetchFileSum(ctx context.Context, model, rel, want string) (string, error) {
	dst := filepath.Join(d.Dir, filepath.FromSlash(rel))
	if sum, err := hashFile(dst); err == nil {
		if want == "" || strings.EqualFold(sum, want) {
			return sum, nil // already here and valid
		}
		// Present but wrong: start again rather than resume onto bad bytes.
		os.Remove(dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}

	part := dst + ".part"
	var have int64
	if st, err := os.Stat(part); err == nil {
		have = st.Size()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.baseURL()+rel, nil)
	if err != nil {
		return "", err
	}
	if have > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(have, 10)+"-")
	}
	resp, err := d.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("voices: download %s: %w", rel, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		have = 0 // server ignored the range; start over
	case http.StatusPartialContent:
	default:
		return "", fmt.Errorf("voices: download %s: %s", rel, resp.Status)
	}

	flags := os.O_CREATE | os.O_WRONLY
	if have > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return "", err
	}

	total := resp.ContentLength
	if total > 0 {
		total += have
	}
	pw := &progressWriter{
		w: f, done: have, total: total,
		report: func(done, tot int64) {
			if d.OnProgress != nil {
				d.OnProgress(Progress{Model: model, File: rel, Downloaded: done, Total: tot})
			}
		},
	}
	if _, err := io.Copy(pw, resp.Body); err != nil {
		f.Close()
		return "", fmt.Errorf("voices: download %s: %w", rel, err)
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	sum, err := hashFile(part)
	if err != nil {
		return "", err
	}
	if want != "" && !strings.EqualFold(sum, want) {
		os.Remove(part)
		return "", fmt.Errorf("voices: %s failed checksum: got %s, want %s", rel, sum, want)
	}
	if err := os.Rename(part, dst); err != nil {
		return "", err
	}
	return sum, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// progressWriter throttles progress callbacks to a few per second so a console
// progress bar does not dominate the download.
type progressWriter struct {
	w      io.Writer
	done   int64
	total  int64
	last   time.Time
	report func(done, total int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.done += int64(n)
	if time.Since(p.last) > 200*time.Millisecond {
		p.last = time.Now()
		p.report(p.done, p.total)
	}
	return n, err
}

// Missing reports which models in the manifest are not on disk yet.
func (p *Pool) Missing() []string {
	var out []string
	for _, m := range p.man.Models {
		if !isInstalled(p.opt.Dir, m) {
			out = append(out, m.Name)
		}
	}
	return out
}

// Manifest exposes the underlying manifest.
func (p *Pool) Manifest() *Manifest { return p.man }
