package voices_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mrlm-net/voice-goio/voices"
)

// Dir is the per-user folder, never the working directory.
func TestDirIsPerUser(t *testing.T) {
	d := voices.Dir()
	if !filepath.IsAbs(d) {
		t.Fatalf("Dir %q is not absolute", d)
	}
	if filepath.Base(d) != "voices" || filepath.Base(filepath.Dir(d)) != "voice-goio" {
		t.Errorf("Dir %q, want …/voice-goio/voices", d)
	}
	if runtime.GOOS == "windows" {
		if la := os.Getenv("LOCALAPPDATA"); la != "" && !strings.EqualFold(d, filepath.Join(la, "voice-goio", "voices")) {
			t.Errorf("Dir %q, want under %%LOCALAPPDATA%% (%s)", d, la)
		}
	}
}

// Installed lists the models whose .onnx is in the folder, in manifest order.
func TestInstalled(t *testing.T) {
	m, err := voices.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if got := voices.Installed(m, dir); len(got) != 0 {
		t.Fatalf("empty folder: %d installed", len(got))
	}
	want := []string{"en_GB-alan-medium", "en_US-ryan-medium"}
	for _, name := range want {
		mod, ok := m.Model(name)
		if !ok {
			t.Fatalf("%s not in the manifest", name)
		}
		onnx := filepath.Join(dir, filepath.FromSlash(mod.ONNX))
		if err := os.MkdirAll(filepath.Dir(onnx), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(onnx, []byte("onnx"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := voices.Installed(m, dir)
	if len(got) != len(want) {
		t.Fatalf("installed %d, want %d", len(got), len(want))
	}
	idx := map[string]int{}
	for i, mod := range m.Models {
		idx[mod.Name] = i
	}
	for i, mod := range got {
		if mod.Name != want[0] && mod.Name != want[1] {
			t.Errorf("installed %q", mod.Name)
		}
		if i > 0 && idx[got[i-1].Name] > idx[mod.Name] {
			t.Error("not in manifest order")
		}
	}
	// The pool agrees: what is installed is not missing.
	p := voices.NewPool(m, voices.PoolOptions{Dir: dir})
	if n := len(p.Missing()); n != len(m.Models)-len(want) {
		t.Errorf("missing %d, want %d", n, len(m.Models)-len(want))
	}
}

// Profile is the model's speaker as an explicit voice.
func TestModelProfile(t *testing.T) {
	m, err := voices.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	mod, ok := m.Model("en_GB-alan-medium")
	if !ok {
		t.Fatal("en_GB-alan-medium not in the manifest")
	}
	v := mod.Profile(0)
	if v.Model != mod.Name || v.SpeakerID != 0 || v.Accent == "" || v.Radio != "" || v.LengthScale != 0 {
		t.Errorf("profile %+v", v)
	}
	for _, mod := range m.Models {
		for _, s := range mod.Speakers {
			if s.Accent != "" && s.Accent != mod.Accent {
				if v := mod.Profile(s.ID); v.Accent != s.Accent || v.SpeakerID != s.ID {
					t.Errorf("%s#%d: %+v, want accent %s", mod.Name, s.ID, v, s.Accent)
				}
				return
			}
		}
	}
}
