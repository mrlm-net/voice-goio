---
title: Platform Status
description: What runs where — macOS, Windows, Linux — and what has not been tried yet.
order: 20
section: reference
---

| Component | macOS | Windows | Linux |
|---|---|---|---|
| normalise, radio chain, voices, grammar parser | ✅ | ✅ | ✅ |
| TTS | ✅ `say` (dev) + piper | ✅ piper, in use in both consumers | ✅ piper |
| Playback (`audio`, `speaker`) | ✅ afplay, default device only | ✅ `winmm`, device per channel, in use in both consumers | WAV files |
| Recognition | ✅ `stt/fake` | ⏳ `stt/sapi` — written, **not exercised by current consumers**, never run against a live engine | `stt/fake` |

The consumers are the MyCrew app and the simconnect airport map; both run piper, the speaker and `winmm` playback on Windows hardware. Neither imports `stt/...`.

## `stt/sapi`

It has never run against a live engine. It is complete — COM creation, audio input selection, grammar load, dynamic callsign rule, PTT, event pump, semantic property extraction — and it compiles and vets clean for `windows/amd64`. What it has not done is talk to SAPI. The struct layouts are pinned by `layout_windows_test.go`, which runs in CI on `windows-latest`, and every failure path returns a typed error. Expect to spend a session on bring-up.

## `tts/piper`

It runs against the real piper binary on Windows in both consumers (sentence-at-a-time synthesis since v0.11.1). The tests still run against `testdata/fakepiper`, which speaks the same protocol.

## Acceptance status

Against the criteria of the [design spec](SPEC.md) §9:

| Criterion | Status |
|---|---|
| `go.mod` zero requires; `CGO_ENABLED=0` builds for darwin/arm64, windows/amd64, linux/amd64 | ✅ enforced by test and CI |
| Normaliser covers every row of §4.1 | ✅ `testdata/normalise/*.txt`, one deviation documented in [Design notes](design-notes.md) |
| ≥ 100 voices, ≥ 10 accents | ✅ 1145 voices, 23 accents (`voicecheck voices`) |
| ≥ 95 % intent + callsign on the corpus; off-grammar yields `say_again` | ✅ 34/34 on `stt/fake` (`voicecheck recog`) |
| Radio profiles audibly distinct; speech intelligible through `center` | ✅ asserted in `audio/radio`, audible via `demo -mode profiles` |
| macOS output documented as default-device only | ✅ `SetDevice` is a no-op there and says so |
| Windows output device selection | ✅ `winmm`, per channel (`SetDeviceFor`), in use in both consumers |
| Network disabled: full flow on Windows | ⏳ output side runs offline once piper and the models are installed; recognition needs SAPI bring-up |
| TTS first sample < 300 ms warm | ⏳ not measured |
