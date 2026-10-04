package piper

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The piper release Install fetches: rhasspy/piper 2023.11.14-2 (MIT),
// https://github.com/rhasspy/piper/releases/tag/2023.11.14-2, its Windows
// build, pinned by SHA-256 (measured 2026-10-04; its piper.exe is the one
// the simconnect airport map ships).
const (
	ReleaseURL    = "https://github.com/rhasspy/piper/releases/download/2023.11.14-2/piper_windows_amd64.zip"
	ReleaseSHA256 = "f3c58906402b24f3a96d92145f58acba6d86c9b5db896d207f78dc80811efcea"
)

// Install downloads the pinned piper release, checks its SHA-256 and unzips
// it into dir: dir/piper/piper.exe with its DLLs and espeak-ng-data, the
// layout speaker.Options.PiperPath defaults to (bin/piper/piper.exe next to
// the executable: pass dir = <exe dir>/bin). progress (nil: none) gets the
// bytes downloaded and the total. Nothing is done when dir/piper/piper.exe
// is there already.
func Install(ctx context.Context, dir string, progress func(done, total int64)) error {
	if _, err := os.Stat(filepath.Join(dir, "piper", "piper.exe")); err == nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	zipPath := filepath.Join(dir, "piper_windows_amd64.zip.part")
	defer os.Remove(zipPath)
	if err := download(ctx, ReleaseURL, zipPath, progress); err != nil {
		return err
	}
	if err := verify(zipPath, ReleaseSHA256); err != nil {
		return err
	}
	return unzip(zipPath, dir)
}

func download(ctx context.Context, url, dst string, progress func(done, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 30 * time.Minute}).Do(req)
	if err != nil {
		return fmt.Errorf("piper: download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("piper: download: %s", resp.Status)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	var done int64
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			done += int64(n)
			if progress != nil {
				progress(done, resp.ContentLength)
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return fmt.Errorf("piper: download: %w", rerr)
		}
	}
}

func verify(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want) {
		return fmt.Errorf("piper: release SHA-256 %s, want %s", got, want)
	}
	return nil
}

// unzip extracts path into dir, refusing entries that would land outside it.
func unzip(path, dir string) error {
	z, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer z.Close()
	root, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for _, e := range z.File {
		dst := filepath.Join(root, filepath.FromSlash(e.Name))
		if dst != root && !strings.HasPrefix(dst, root+string(os.PathSeparator)) {
			return fmt.Errorf("piper: %s leaves the folder", e.Name)
		}
		if e.FileInfo().IsDir() {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := extract(e, dst); err != nil {
			return err
		}
	}
	return nil
}

func extract(e *zip.File, dst string) error {
	src, err := e.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
