---
title: Getting Started
description: What voice-goio is, how to add it to a Go application, and the demos that make it audible.
order: 1
section: getting-started
---

voice-goio is offline ATC voice input and output, as a Go library: controller speech out, pilot speech in, for any application that needs a radio — a flight simulator add-on, a training tool, a controller trainer.

The library is aviation-specific — the normaliser, the grammar, the radio chain and the voice pool are all built around ATC phraseology — but it knows nothing about any particular simulator. There is no SimConnect here, no MSFS, no assumption about where the transmissions come from. An application hands it text and gets audio; it hands the application tags.

> **Under active development.** The API is pre-1.0 and versions come fast. The output side (`tts/piper`, `speaker`, `audio` on `winmm`) runs on Windows hardware in two applications; recognition (`stt/sapi`) is not exercised by current consumers and has never run against a live engine. See [Platform status](platform-status.md).

- **No services.** Everything runs on the user's PC. Works with the network adapter disabled.
- **No dependencies.** `go.mod` has zero `require` lines. Standard library only, `CGO_ENABLED=0` everywhere.
- **Windows is the runtime target** (the first consumer is an MSFS add-on, and MSFS is Windows-only), macOS is the development machine. Windows-only code is cross-compiled on every commit so it cannot rot.

The application deals in **text and semantic tags**. It never sees PCM, grammars or voice model files.

```
app text ─▶ normalise ─▶ TTS ─▶ radio chain ─▶ player ─▶ PlaybackEvent
pilot speech ─▶ STT ─▶ Recognition{intent, callsign, value} ─▶ app
```

## Install

Requires Go 1.27 or newer.

```bash
go get github.com/mrlm-net/voice-goio@v0.13.1
```

Most applications only need the [`speaker`](speaker.md) package, plus [piper and a voice pack](piper-and-voices.md) installed on the user's machine. [Integrating](integrating.md) covers developing the library and an application side by side.

## Hear it: the demos

```bash
make demo-arrival   # the full sequence: radar → approach → tower → ground
make demo-emergency # a mayday with a second aircraft on the same runway
make demo           # one arrival, with the weather going off and a new ATIS
make demo-accents   # one clearance in many assigned voices
make demo-profiles  # one clearance through each radio profile
make demo-live      # type pilot calls, hear the controller answer
make demo-wav       # render every demo to one WAV each, silently
```

`demo-arrival` is the one to run first. It is the whole library in one pass: four controller positions, four frequencies, three handoffs, and all twelve pilot calls put through the recogniser with their tags printed next to the audio that produced them.

On macOS with no piper installed the demo falls back to the built-in `say` backend, so the whole pipeline is audible before anything has been downloaded. On Windows it uses piper.

## Build and test

```bash
make bars      # the two machine-checkable quality bars
make all       # fmt, vet, test, and the three cross builds
make deps      # prove go.mod is empty and nothing outside stdlib is reachable
```

## Next

- [The speaker](speaker.md) — the radio, intercom and cabin PA as applications use them.
- [Piper and voices](piper-and-voices.md) — installing the synthesiser and the voice models.
- [The pipeline](pipeline.md) — the lower-level pieces the speaker is built from.
