---
title: The Pipeline
description: Synthesis, normalisation, the radio chain and playback, wired by hand.
order: 10
section: guides
---

The [`speaker`](speaker.md) assembles all of this for you. This page is the layer below it, for an application that wants to wire the pieces itself.

The snippets are compiled on every build — they live in [`example_test.go`](https://github.com/mrlm-net/voice-goio/blob/main/example_test.go), so they cannot drift from the API.

```go
import (
    voicegoio "github.com/mrlm-net/voice-goio"
    "github.com/mrlm-net/voice-goio/audio"
    "github.com/mrlm-net/voice-goio/audio/radio"
    "github.com/mrlm-net/voice-goio/normalise"
    "github.com/mrlm-net/voice-goio/tts"
    "github.com/mrlm-net/voice-goio/voices"
)

engine, _, _ := tts.Open(tts.Options{})          // piper on Windows
man, _       := voices.LoadDefault()
pool         := voices.NewPool(man, voices.PoolOptions{Seed: sessionSeed})
chain        := radio.Default()
player, _    := audio.NewPlayer(audio.Options{DeviceID: settings.OutputDevice})
norm         := normalise.New()

// One ATC transmission.
voice := pool.Assign("LKPR", voicegoio.Tower)              // stable for the session
tx := voicegoio.Transmission{
    Frequency: "118.100", ControllerID: "LKPR_TWR",
    Phraseology: voicegoio.ICAO,
    Text: "BAW123 RWY 24 cleared to land, wind 250 degrees 8 kt",
}
pcm, _ := engine.Synthesize(ctx, voice, norm.Spoken(tx.Text, tx.Phraseology))
out := chain.Apply(pcm, engine.SampleRate(voice), voice.Radio, player.SampleRate(), seed)
player.Play(tx, out, player.SampleRate())

// Playback events tell you when the frequency is clear again.
for ev := range player.Events() { ... }
```

## The stages

| Stage | Package | What it does |
|---|---|---|
| Normalise | `normalise` | `BAW123 climb FL350` → `Speedbird one two tree, climb flight level tree fife zero`. ICAO and FAA, emergency signals, METAR shorthand and the pauses. See [Phraseology and prosody](phraseology.md). |
| Synthesise | `tts` | Selects a backend: `tts/piper` (shipping), `tts/say` (macOS development), `tts/fake` (deterministic tones for CI). |
| Radio chain | `audio/radio` | Band pass, presence, soft clip, noise, squelch, dropouts, level, resample. |
| Play | `audio` | Per-frequency queues and playback events; `winmm` on Windows, `afplay` on macOS, WAV files elsewhere, a silent sink for rendering. |

## Loudness, not peaks

The radio chain compresses hard and normalises to −15 dBFS RMS under a −3 dBFS soft ceiling. Normalising peaks instead leaves speech around −20 dBFS RMS because of its 15 dB crest factor, and it sounds distant and hard to follow. Compression is also what real transmitters do, and it is the single biggest intelligibility win in the chain. `internal/dsp` has the stage-by-stage level test.

`radio.PeakDBFS(pcm)` and `radio.RMSdBFS(pcm)` report a signal's peak and RMS level in dBFS; `radio.Load(b)` reads radio profiles.

## What to wire first

1. **Playback.** `tts.Open` → `pool.Assign` → `norm.Spoken` → `chain.Apply` → `player.Play`, and drain `player.Events()`. This works on macOS with no downloads, so the integration can be proven before any Windows work.
2. **Settings.** `player.Devices()` for the output device; on Windows, `sapi.InputDevices()` for the microphone and `sapi.Engines()` to show whether a speech pack is installed at all.
3. **Voices.** `voicecheck download -model all` once, or call `voices.Downloader` from the app's first-run screen. `pool.Missing()` reports what has not been fetched.
4. **Recognition.** `stt/fake` on the development machine, `stt/sapi` on Windows. Same interface, so the wiring is written once. See [Recognition](recognition.md).
