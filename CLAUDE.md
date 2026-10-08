# voice-goio

Offline ATC voice in and out, as a Go library (`github.com/mrlm-net/voice-goio`). Text in, radio audio out; pilot speech in, intent tags out. Knows nothing about MSFS or SimConnect. README.md is current; docs/SPEC.md is the historic design spec.

## Packages
- `voicegoio.go` — public types and interfaces (VoiceProfile, ControllerKind, ICAO/FAA, Transmission); the compatibility surface.
- `speaker/` — what apps use: radio (one frequency, queue, gaps, ATIS), intercom, PA, chimes, device per channel, Clip/WAV.
- `voices/` — manifest, packs + InstallPack, Downloader, Installed/Dir, Pool (Assign, AssignCrew).
- `tts/piper` (+ `Install`), `tts/say` (macOS dev), `tts/fake`, `tts` (backend selection).
- `stt/sapi` (Windows SAPI over raw COM, never run live, no consumer uses it), `stt/fake`.
- `normalise/` text to spoken ATC; `grammar/`, `data/` embedded SRGS + telephony; `audio/` players; `audio/radio` radio chain.
- `internal/` dsp, wav, userdir (per-user folder), jsonl (piper stdin protocol). `cmd/demo`, `cmd/voicecheck` (tools).

## Consumers (both pin v0.13.1)
- `C:\msfs-development\mycrew-online\app` — `internal/voice/*.go`: speaker (radio, intercom, PA, chimes, SetDeviceFor, WAV), voices (LoadDefault, Installed, Dir, Packs, PackCore, InstallPack, Model), piper.Install.
- `C:\msfs-development\simconnect\cmd\airport-map` — `voice.go`, `network.go`: speaker (New, Utterance with Controller, State, OnSay), speaker.WAV for browser clients.

## Build and test
- `make all` (fmt, vet, test, cross builds for windows/darwin/linux + `-tags commercial`), `make test`, `make deps` (stdlib-only check), `make release-check` (clean clone build/test; run before tagging).
- Plain: `go build ./... && go vet ./... && go test ./...` with `CGO_ENABLED=0`.

## Conventions
- Stdlib only: zero `require` lines in go.mod (`deps_test.go` enforces). No cgo; Windows APIs (winmm, SAPI COM) via `syscall.NewLazyDLL`.
- Windows-only code lives in `_windows.go` files with stubs elsewhere, cross-compiled every build.
- Voice models and piper are downloaded, never committed (`/bin/`, `*.exe` gitignored).

## Release
- Features = minor, fixes = patch. Add a CHANGELOG.md entry (Keep a Changelog, `## [x.y.z] — date`) and a README Versioning row.
- Squash-merge the PR, verify the squash commit, then tag it `vX.Y.Z`.
- Then bump `github.com/mrlm-net/voice-goio` in `mycrew-online/app/go.mod` and `simconnect/cmd/airport-map/go.mod`.
