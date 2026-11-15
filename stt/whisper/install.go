package whisper

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Release is the whisper.cpp build Install fetches: ggml-org/whisper.cpp
// b5454 (MIT, the build of v1.9.5), https://github.com/ggml-org/whisper.cpp/releases/tag/b5454.
// macOS has no whisper-cli there; ours is built from the same tag by
// voice-goio's whisper-macos workflow into the voice-goio release
// "whisper-b5454".
const Release = "b5454"

// Asset is one platform's download: an archive, its SHA-256, and the
// folder in it holding whisper-cli ("" the archive's root).
type Asset struct {
	URL, SHA256, Dir string
}

// Assets by GOOS/GOARCH (SHA-256 measured 2026-11-15).
var Assets = map[string]Asset{
	"windows/amd64": {"https://github.com/ggml-org/whisper.cpp/releases/download/b5454/whisper-bin-x64.zip",
		"6ba69e3482d7826214f90a6a9c84ca07782aec1e1d0c6a7c30c994fd5d816ccb", "Release"},
	"linux/amd64": {"https://github.com/ggml-org/whisper.cpp/releases/download/b5454/whisper-bin-ubuntu-x64.tar.gz",
		"a72becf15d7917f990f6313867a52638b82b7f9ef237fb0c980dac56a135781c", "whisper-bin-ubuntu-x64"},
	"linux/arm64": {"https://github.com/ggml-org/whisper.cpp/releases/download/b5454/whisper-bin-ubuntu-arm64.tar.gz",
		"6b95ebfc60447df48e70ef00a73bdc3f41679ed2d01ef465827206a2ff325149", "whisper-bin-ubuntu-arm64"},
	"darwin/arm64": {"https://github.com/mrlm-net/voice-goio/releases/download/whisper-b5454/whisper-cli-darwin-arm64.tar.gz",
		darwinArm64SHA256, ""},
	"darwin/amd64": {"https://github.com/mrlm-net/voice-goio/releases/download/whisper-b5454/whisper-cli-darwin-amd64.tar.gz",
		darwinAmd64SHA256, ""},
}

// The macOS builds' SHA-256, pinned once the whisper-macos workflow has
// published them; "" until then (Install refuses, a Homebrew whisper-cli
// still works).
const (
	darwinArm64SHA256 = ""
	darwinAmd64SHA256 = ""
)

// binName is whisper-cli's file name on this platform.
func binName() string {
	if runtime.GOOS == "windows" {
		return "whisper-cli.exe"
	}
	return "whisper-cli"
}

// Find is the whisper-cli to run: dir/whisper/whisper-cli (Install's
// layout), else one on PATH (Homebrew's on macOS: brew install whisper-cpp).
func Find(dir string) (string, error) {
	p := filepath.Join(dir, "whisper", binName())
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("whisper-cli"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("whisper: no whisper-cli in %s or on PATH (whisper.Install, or on macOS brew install whisper-cpp)", filepath.Join(dir, "whisper"))
}

// Install puts whisper-cli for this platform into dir/whisper (with its
// libraries) and returns its path; nothing is fetched when it is there. On
// macOS a whisper-cli on PATH (Homebrew) is returned instead of fetching
// ours. progress (nil: none) gets the bytes downloaded and the total.
func Install(ctx context.Context, dir string, progress func(done, total int64)) (string, error) {
	p := filepath.Join(dir, "whisper", binName())
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	if runtime.GOOS == "darwin" {
		if brew, err := exec.LookPath("whisper-cli"); err == nil {
			return brew, nil
		}
	}
	a, ok := Assets[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return "", fmt.Errorf("whisper: no build for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if a.SHA256 == "" {
		return "", fmt.Errorf("whisper: the %s/%s build is not published yet; brew install whisper-cpp", runtime.GOOS, runtime.GOARCH)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	part := filepath.Join(dir, path.Base(a.URL)+".part")
	defer os.Remove(part)
	if err := download(ctx, a.URL, part, progress); err != nil {
		return "", err
	}
	if err := verify(part, sha256.New(), a.SHA256); err != nil {
		return "", err
	}
	if err := extract(part, a.Dir, filepath.Join(dir, "whisper")); err != nil {
		return "", err
	}
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("whisper: %s not in %s", binName(), path.Base(a.URL))
	}
	return p, nil
}

// Model is a ggml model: its file and SHA-1 (whisper.cpp's models/README.md).
type Model struct {
	Name, SHA1 string
}

// Models are the English ggml models InstallModel knows: tiny.en (75 MiB),
// base.en (142 MiB, the default), small.en (466 MiB).
var Models = map[string]Model{
	"tiny.en":  {"ggml-tiny.en.bin", "c78c86eb1a8faa21b369bcd33207cc90d64ae9df"},
	"base.en":  {"ggml-base.en.bin", "137c40403d78fd54d454da0f9bd998f78703390c"},
	"small.en": {"ggml-small.en.bin", "db8a495a91d927739e50b3fc1cc4c6b8f6c2d022"},
}

// ModelURL is where models come from, as whisper.cpp's download script.
const ModelURL = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/"

// InstallModel downloads model name ("" base.en) into dir, checks its
// SHA-1 and returns its path; nothing is fetched when it is there.
func InstallModel(ctx context.Context, dir, name string, progress func(done, total int64)) (string, error) {
	if name == "" {
		name = "base.en"
	}
	m, ok := Models[name]
	if !ok {
		return "", fmt.Errorf("whisper: unknown model %q", name)
	}
	p := filepath.Join(dir, m.Name)
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	part := p + ".part"
	defer os.Remove(part)
	if err := download(ctx, ModelURL+m.Name, part, progress); err != nil {
		return "", err
	}
	if err := verify(part, sha1.New(), m.SHA1); err != nil {
		return "", err
	}
	return p, os.Rename(part, p)
}

func download(ctx context.Context, url, dst string, progress func(done, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 30 * time.Minute}).Do(req)
	if err != nil {
		return fmt.Errorf("whisper: download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("whisper: download %s: %s", url, resp.Status)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	var r io.Reader = resp.Body
	if progress != nil {
		r = &counter{r: resp.Body, total: resp.ContentLength, fn: progress}
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return fmt.Errorf("whisper: download: %w", err)
	}
	return f.Close()
}

type counter struct {
	r     io.Reader
	done  int64
	total int64
	fn    func(done, total int64)
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.done += int64(n)
	c.fn(c.done, c.total)
	return n, err
}

func verify(p string, h hash.Hash, want string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("whisper: %s checksum %s, want %s", filepath.Base(p), got, want)
	}
	return nil
}

// extract unpacks the files under sub ("" all) of a .zip or .tar.gz into
// dst, flat, keeping the tar's symbolic links (the Linux .so chain).
func extract(archive, sub, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	name := func(n string) (string, bool) {
		n = strings.TrimPrefix(path.Clean("/"+n), "/")
		if sub != "" {
			if !strings.HasPrefix(n, sub+"/") {
				return "", false
			}
			n = strings.TrimPrefix(n, sub+"/")
		}
		if n == "" || n == "." || strings.Contains(n, "/") {
			return "", false
		}
		return n, true
	}
	if strings.HasSuffix(strings.TrimSuffix(archive, ".part"), ".zip") {
		z, err := zip.OpenReader(archive)
		if err != nil {
			return err
		}
		defer z.Close()
		for _, f := range z.File {
			n, ok := name(f.Name)
			if !ok || f.FileInfo().IsDir() {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			err = writeFile(filepath.Join(dst, n), rc, 0o755)
			rc.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		n, ok := name(h.Name)
		if !ok {
			continue
		}
		target := filepath.Join(dst, n)
		switch h.Typeflag {
		case tar.TypeReg:
			if err := writeFile(target, tr, os.FileMode(h.Mode)&0o777|0o600); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if strings.Contains(h.Linkname, "/") || strings.Contains(h.Linkname, `\`) {
				continue
			}
			os.Remove(target)
			if err := os.Symlink(h.Linkname, target); err != nil {
				return err
			}
		}
	}
}

func writeFile(p string, r io.Reader, mode os.FileMode) error {
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
