package voices

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPacks: English only and all; a pack zip with the models' files and a
// manifest of just them; a model not installed is an error (#17).
func TestPacks(t *testing.T) {
	m, err := LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	en, _ := m.Pack(PackEnglish)
	all, _ := m.Pack(PackAll)
	if len(en) == 0 || len(all) <= len(en) {
		t.Fatalf("en %d, all %d", len(en), len(all))
	}
	for _, mod := range en {
		if !strings.HasPrefix(mod.Name, "en_") {
			t.Errorf("%s in the English pack", mod.Name)
		}
	}
	if _, err := m.Pack("xx"); err == nil {
		t.Error("an unknown pack")
	}
	dir := t.TempDir()
	one := en[:1]
	for _, rel := range []string{one[0].ONNX, one[0].Config} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(rel), 0o644)
	}
	var buf bytes.Buffer
	if err := m.WritePack(&buf, one, dir); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names[one[0].ONNX] || !names[one[0].Config] || !names["voices.json"] || len(names) != 3 {
		t.Errorf("zip %v", names)
	}
	if err := m.WritePack(&bytes.Buffer{}, en[:2], dir); err == nil {
		t.Error("a pack with a model not installed")
	}
}
