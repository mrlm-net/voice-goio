package piper

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestUnzipStaysInside: an entry leaving the folder is refused; the rest
// lands under it.
func TestUnzipStaysInside(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "a.zip")
	mk := func(names ...string) {
		f, _ := os.Create(zp)
		z := zip.NewWriter(f)
		for _, n := range names {
			w, _ := z.Create(n)
			w.Write([]byte(n))
		}
		z.Close()
		f.Close()
	}
	mk("piper/piper.exe", "piper/espeak-ng-data/x")
	out := filepath.Join(dir, "out")
	if err := unzip(zp, out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "piper", "espeak-ng-data", "x")); err != nil {
		t.Error(err)
	}
	mk("../evil")
	if err := unzip(zp, out); err == nil {
		t.Error("an entry leaving the folder was extracted")
	}
}

// TestInstallSkips: nothing is fetched when piper is there.
func TestInstallSkips(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "piper"), 0o755)
	os.WriteFile(filepath.Join(dir, "piper", "piper.exe"), []byte("x"), 0o644)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // any network use would fail
	if err := Install(ctx, dir, nil); err != nil {
		t.Fatal(err)
	}
}
