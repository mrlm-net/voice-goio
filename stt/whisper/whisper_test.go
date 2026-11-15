package whisper

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	voicegoio "github.com/mrlm-net/voice-goio"
)

// fakeBin builds testdata/fakewhisper: whisper-cli's stand-in printing
// <wav>.txt.
func fakeBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "whisper-cli")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, filepath.Join("..", "..", "testdata", "fakewhisper", "main.go")).CombinedOutput(); err != nil {
		t.Fatalf("build fakewhisper: %v\n%s", err, out)
	}
	return bin
}

// said writes a WAV whose transcript is text.
func said(t *testing.T, text string) string {
	t.Helper()
	wav := filepath.Join(t.TempDir(), "ptt.wav")
	os.WriteFile(wav, make([]byte, 100), 0o644)
	os.WriteFile(wav+".txt", []byte(text), 0o644)
	return wav
}

func newRec(t *testing.T, controller bool) *Recognizer {
	t.Helper()
	model := filepath.Join(t.TempDir(), "ggml-base.en.bin")
	os.WriteFile(model, []byte("model"), 0o644)
	r, err := New(Options{Bin: fakeBin(t), Model: model, Controller: controller})
	if err != nil {
		t.Fatal(err)
	}
	r.SetCallsigns([]voicegoio.Callsign{{ICAO: "BAW123"}, {ICAO: "DLH4EK"}})
	return r
}

// whisper's text, written with numerals, is tagged as SAPI would tag it,
// on either side; the callsigns are in the prompt.
func TestRecognizeFile(t *testing.T) {
	ctl := newRec(t, true)
	for text, want := range map[string]map[string]string{
		"Speedbird 123, runway 24, cleared to land, wind 240 degrees, 8 knots.": {"intent": "land", "callsign": "BAW123", "value": "24", "wind": "240/8"},
		"Lufthansa 4EK, contact tower 118.1.":                                   {"intent": "handoff", "callsign": "DLH4EK", "value": "118.1", "station": "tower"},
		"Speedbird 123, taxi to holding point A1, runway 24, via B, A.":         {"intent": "taxi", "callsign": "BAW123", "value": "24", "holding_point": "A1", "via": "B A"},
	} {
		got := ctl.RecognizeFile(said(t, text))
		for k, v := range want {
			if got.Tags[k] != v {
				t.Errorf("%q: %s = %q, want %q (%v)", text, k, got.Tags[k], v, got.Tags)
			}
		}
	}
	pilot := newRec(t, false)
	wav := said(t, "Descending FL100, Speedbird 123.")
	got := pilot.RecognizeFile(wav)
	if got.Tags["intent"] != "readback_altitude" || got.Tags["value"] != "FL100" || got.Tags["callsign"] != "BAW123" {
		t.Errorf("pilot: %v", got.Tags)
	}
	args, _ := os.ReadFile(wav + ".args")
	if !strings.Contains(string(args), "Speedbird one two three") || !strings.Contains(string(args), "-nt") {
		t.Errorf("args:\n%s", args)
	}
}

// Nothing heard, or the binary failing, is say_again.
func TestRecognizeFileNothing(t *testing.T) {
	r := newRec(t, true)
	if got := r.RecognizeFile(said(t, "")); got.Tags["intent"] != voicegoio.IntentSayAgain {
		t.Errorf("empty: %v", got.Tags)
	}
	if got := r.RecognizeFile(filepath.Join(t.TempDir(), "missing.wav")); got.Tags["intent"] != voicegoio.IntentSayAgain {
		t.Errorf("failed: %v", got.Tags)
	}
}

// A push to talk cycle: the recorder's file is transcribed, one result per
// Stop, the file removed.
func TestPushToTalk(t *testing.T) {
	r := newRec(t, true)
	wav := said(t, "Speedbird 123, cross runway 12.")
	r.opt.Recorder = &fileRecorder{path: wav}
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
	got := <-r.Results()
	if got.Tags["intent"] != "cross" || got.Tags["value"] != "12" {
		t.Errorf("%v", got.Tags)
	}
	r.Close()
	if _, err := os.Stat(wav); err == nil {
		t.Error("recording kept")
	}
}

type fileRecorder struct{ path string }

func (f *fileRecorder) Start() error          { return nil }
func (f *fileRecorder) Stop() (string, error) { return f.path, nil }

func TestNewNeedsModel(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Error("no model: no error")
	}
	if _, err := New(Options{Model: filepath.Join(t.TempDir(), "none.bin")}); err == nil {
		t.Error("missing model: no error")
	}
}

// extract takes the files under the archive's folder, flat; a tar keeps
// its symbolic links.
func TestExtract(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "w.zip")
	zf, _ := os.Create(zp)
	zw := zip.NewWriter(zf)
	for _, n := range []string{"Release/whisper-cli.exe", "Release/ggml.dll", "other/skip.txt", "Release/sub/deep.dll"} {
		w, _ := zw.Create(n)
		w.Write([]byte(n))
	}
	zw.Close()
	zf.Close()
	out := filepath.Join(dir, "zip")
	if err := extract(zp, "Release", out); err != nil {
		t.Fatal(err)
	}
	if names := list(t, out); names != "ggml.dll whisper-cli.exe" {
		t.Errorf("zip: %s", names)
	}

	tp := filepath.Join(dir, "w.tar.gz")
	tf, _ := os.Create(tp)
	gz := gzip.NewWriter(tf)
	tw := tar.NewWriter(gz)
	for _, n := range []string{"b/whisper-cli", "b/libwhisper.so.1.9.5", "../evil"} {
		tw.WriteHeader(&tar.Header{Name: n, Mode: 0o755, Size: int64(len(n)), Typeflag: tar.TypeReg})
		tw.Write([]byte(n))
	}
	links := runtime.GOOS != "windows"
	if links {
		tw.WriteHeader(&tar.Header{Name: "b/libwhisper.so.1", Linkname: "libwhisper.so.1.9.5", Typeflag: tar.TypeSymlink})
		tw.WriteHeader(&tar.Header{Name: "b/escape", Linkname: "../../etc/passwd", Typeflag: tar.TypeSymlink})
	}
	tw.Close()
	gz.Close()
	tf.Close()
	out = filepath.Join(dir, "tar")
	if err := extract(tp, "b", out); err != nil {
		t.Fatal(err)
	}
	want := "libwhisper.so.1.9.5 whisper-cli"
	if links {
		want = "libwhisper.so.1 libwhisper.so.1.9.5 whisper-cli"
		if l, err := os.Readlink(filepath.Join(out, "libwhisper.so.1")); err != nil || l != "libwhisper.so.1.9.5" {
			t.Errorf("link %q %v", l, err)
		}
	}
	if names := list(t, out); names != want {
		t.Errorf("tar: %s", names)
	}
}

func list(t *testing.T, dir string) string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var n []string
	for _, e := range es {
		n = append(n, e.Name())
	}
	return strings.Join(n, " ")
}

// Every platform has an asset; the pinned ones are SHA-256 hex.
func TestAssets(t *testing.T) {
	for _, p := range []string{"windows/amd64", "linux/amd64", "linux/arm64", "darwin/arm64", "darwin/amd64"} {
		a, ok := Assets[p]
		if !ok {
			t.Errorf("%s: no asset", p)
			continue
		}
		if a.SHA256 != "" && len(a.SHA256) != 64 {
			t.Errorf("%s: sha %q", p, a.SHA256)
		}
		if !strings.Contains(a.URL, Release) {
			t.Errorf("%s: %s not of %s", p, a.URL, Release)
		}
	}
	for n, m := range Models {
		if len(m.SHA1) != 40 || !strings.Contains(m.Name, n) {
			t.Errorf("model %s: %+v", n, m)
		}
	}
}
