---
title: Packages
description: What is where in the module, and which file is the compatibility surface.
order: 21
section: reference
---

| Path | What it does |
|---|---|
| `voicegoio.go` | The whole public API: interfaces and value types (`VoiceProfile`, `ControllerKind`, `ICAO`/`FAA`, `Transmission`). This file is the compatibility surface. |
| `speaker/` | The radio, heard: voices per position, controller and crew, one frequency, the queue, the gaps, the ATIS broadcast — the rules applications share. Beside it the intercom (dry), the cabin PA (`PAChain`), code-generated chimes, an output device per channel, and `WAV`/`Clip` for in-memory audio. |
| `voices/` | Manifest, packs (`core`, `en`, `all`) and `InstallPack`, downloader, `Installed`/`Dir`, region-weighted voice assignment (`Pool.Assign`, `Pool.AssignCrew`). |
| `normalise/` | `BAW123 climb FL350` → `Speedbird one two tree, climb flight level tree fife zero`. ICAO and FAA, plus emergency signals, METAR shorthand and the pauses. |
| `tts/` | Selects a synthesis backend. |
| `tts/piper/` | The shipping synthesiser: a pool of warm piper sidecars, JSON lines in, raw PCM out. `piper.Install` fetches the pinned release for Windows, SHA-256 checked. |
| `tts/say/` | macOS development synthesiser. English voices only, no downloads. Not a shipping backend. |
| `tts/fake/` | Deterministic tone generator for CI and regression. |
| `stt/sapi/` | Windows SAPI 5 in-process recognizer over raw COM vtables. |
| `stt/fake/` | The tag parser plus stdin and script recognisers, for development and regression. |
| `audio/` | Per-frequency queues and playback events; `winmm` on Windows, `afplay` on macOS, WAV files elsewhere, a silent sink for rendering. |
| `audio/radio/` | The radio chain: band pass, presence, soft clip, noise, squelch, dropouts, level, resample. |
| `data/`, `grammar/` | Embedded telephony table and the SRGS grammar, and `grammar.Commands` for an application's own phrases (leaf packages, because `go:embed` cannot reach out of its own directory). |
| `internal/dsp` | Signal processing primitives the radio chain is built from: biquads, resampling, level helpers. |
| `internal/wav` | Read and write 16-bit PCM WAV. |
| `internal/userdir` | The per-user data folder, so `voices` and `tts/piper` agree on where models live. |
| `internal/jsonl` | The JSON-lines protocol written to piper's stdin. |
| `cmd/voicecheck` | Regression and audit CLI. |
| `cmd/demo` | The whole pipeline, assembled and audible. `-mode arrival` is the full one: four positions, four frequencies, three handoffs, every pilot call recognised. |

## The compatibility surface

`voicegoio.go` is the only file the application should depend on the shape of. Everything else is an implementation behind one of its interfaces. Breaking changes to it mean a `/v2` module path; anything else is additive.

`go doc` on any package has the details; [pkg.go.dev](https://pkg.go.dev/github.com/mrlm-net/voice-goio) renders the same.
