// Command fakewhisper stands in for whisper-cli in tests and CI (no binary
// or model is fetched there): for -f in.wav it prints in.wav.txt, the
// transcript the test wants, and writes its arguments to in.wav.args.
//
// It lives under testdata/ so the go tool never builds it as part of the module.
package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	var wav string
	for i, a := range os.Args {
		if a == "-f" && i+1 < len(os.Args) {
			wav = os.Args[i+1]
		}
	}
	if wav == "" {
		fmt.Fprintln(os.Stderr, "fakewhisper: no -f")
		os.Exit(2)
	}
	os.WriteFile(wav+".args", []byte(strings.Join(os.Args[1:], "\n")), 0o644)
	text, err := os.ReadFile(wav + ".txt")
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakewhisper:", err)
		os.Exit(1)
	}
	fmt.Printf("\n %s\n", strings.TrimSpace(string(text)))
}
