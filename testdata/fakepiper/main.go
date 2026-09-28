// Command fakepiper stands in for the real piper binary in tests and CI.
//
// SPEC.md 7 forbids fetching piper or any voice model in CI, but the sidecar
// protocol is exactly the part worth regression testing: JSON lines in, raw
// 16 bit PCM out, one utterance per line with no end marker. This program
// speaks that protocol and emits a deterministic tone whose length encodes the
// number of words, so a test can assert that the sentinel was trimmed correctly.
//
// It lives under testdata/ so the go tool never builds it as part of the module.
package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"flag"
	"math"
	"os"
	"strings"
)

// samplesPerWord is the fake "speaking rate": one word of text is this many
// samples of audio.
const samplesPerWord = 1000

type request struct {
	Text        string   `json:"text"`
	SpeakerID   *int     `json:"speaker_id"`
	LengthScale *float32 `json:"length_scale"`
}

func main() {
	// Accept and ignore the real piper's flags so the command line the library
	// builds is exercised as written.
	flag.String("model", "", "")
	flag.String("config", "", "")
	flag.Bool("output-raw", false, "")
	flag.Bool("json-input", false, "")
	flag.String("sentence_silence", "", "")
	flag.Parse()

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64<<10), 1<<20)
	out := bufio.NewWriter(os.Stdout)
	for in.Scan() {
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			os.Stderr.WriteString("fakepiper: bad json: " + err.Error() + "\n")
			continue
		}
		n := len(strings.Fields(req.Text)) * samplesPerWord
		// A speaker id shifts the pitch, so a test can tell voices apart.
		freq := 220.0
		if req.SpeakerID != nil {
			freq += float64(*req.SpeakerID)
		}
		for i := range n {
			v := int16(20000 * math.Sin(2*math.Pi*freq*float64(i)/22050))
			binary.Write(out, binary.LittleEndian, v)
		}
		out.Flush()
	}
}
