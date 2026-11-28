---
title: voicecheck CLI
description: The regression and audit tool — synthesis, accent audits, the recognition corpus, devices, the voice pool, downloads and installer packs.
order: 23
section: reference
---

`cmd/voicecheck` is the library's regression and audit tool. Run it from the repository root:

```bash
go run ./cmd/voicecheck <command> [flags]
go run ./cmd/voicecheck <command> -h   # the flags of one command
```

Every command that reads voices takes `-voices` (a manifest path; the embedded `voices.json` by default) and `-dir` (the voice model folder; `voices.Dir()` by default).

| Command | What it does |
|---|---|
| `synth` | Synthesise the audit text in every voice and radio profile, one WAV each. |
| `audit` | Render the audit text in every speaker of one non-English model, for the swap-trick listening pass. |
| `recog` | Replay a recognition corpus and compare the tags. |
| `devices` | List the audio input and output devices. |
| `voices` | Report the pool size, accent coverage and missing models against the bar. |
| `download` | Fetch models from the voice repository. |
| `pack` | Zip a voice pack of the installed models, for an installer. |

## synth

```bash
go run ./cmd/voicecheck synth -tts piper -limit 0 -profiles tower,center
```

| Flag | Default | Meaning |
|---|---|---|
| `-text` | `testdata/audit/clearance.txt` | The text to speak. |
| `-out` | `wav` | Output directory. |
| `-tts` | | Backend: `piper`, `say` or `fake`. |
| `-limit` | `12` | Maximum number of voices (0 = no limit). |
| `-profiles` | all | Comma-separated radio profiles. |

## audit

```bash
go run ./cmd/voicecheck audit -model de_DE-thorsten-medium
```

`-model` is required. `-text`, `-out` (default `wav-audit`) and `-tts` as for `synth`. The audit fields in the manifest (`quality`, `pass`) are filled in by the human listening pass this renders for; see [Voice assignment](voice-assignment.md#unaudited-speakers).

## recog

```bash
go run ./cmd/voicecheck recog                  # the corpus on stt/fake
go run ./cmd/voicecheck recog -backend sapi -wav wav-recog
go run ./cmd/voicecheck recog -backend whisper -tts piper -model ggml-base.en.bin -wav wav-recog
go run ./cmd/voicecheck recog -backend whisper -tts piper -speakers 16 -model ggml-base.en.bin -wav wav-recog
go run ./cmd/voicecheck recog -backend whisper -mic -device "PRO X" -model ggml-base.en.bin
```

| Flag | Default | Meaning |
|---|---|---|
| `-backend` | `fake` | `fake`, `sapi` or `whisper`. |
| `-grammar` | `atc` | `atc` (pilot speech, `testdata/stt/readbacks.jsonl`) or `controller` (`testdata/stt/controller.jsonl`). |
| `-script` | `testdata/stt/readbacks.jsonl` | The corpus. |
| `-min` | `95` | Minimum percentage of exactly tagged phrases; below it the command fails. |
| `-wav` | | Render each phrase to a WAV here and feed the file to the engine (unattended SAPI and whisper runs). |
| `-tts` | | The backend that renders those phrases (`piper`; the fake backend renders silence). |
| `-speakers` | `0` | With `-wav`: the phrases said in turn by this many installed English voices and accents (one speaker per model, foreign models reading English with their accent), with a per-voice summary. 0 or 1: one voice, which measures the grammar rather than the speakers. |
| `-voice` | | With `-wav`: every phrase in this voice, `model[#speaker]` (an accent: `cs_CZ-jirka-medium`). |
| `-model`, `-bin` | | whisper: the ggml model and whisper-cli (default beside the model, else PATH). |
| `-mic`, `-device` | | whisper: say each phrase into the microphone, Enter to start and to stop; `-device` an index or part of the name (`voicecheck devices`). |

## voices

```bash
go run ./cmd/voicecheck voices
```

Fails below `-min` assignable voices (default 100, the BeyondATC "Basic" bar) or `-min-accents` distinct accents (default 10).

## download

```bash
go run ./cmd/voicecheck download -model en_GB-vctk-medium   # one model
go run ./cmd/voicecheck download -pack en                    # a pack
go run ./cmd/voicecheck download -model all -write voices/voices.json
```

One of `-model` (a name, or `all`) or `-pack` (`core`, `en`, `all`) is required. `-base` points at another base URL or a LAN mirror; `-write` writes the manifest back with the SHA-256 hashes filled in.

## pack

```bash
go run ./cmd/voicecheck pack -pack en -out voices-en.zip
```

Writes the zip an installer unpacks into the voices folder: each model's `.onnx` and config at their manifest paths, plus a `voices.json` of just those models. Every model in the pack must be installed in `-dir`; a pack is whole or not made. `-pack` is `core`, `en` or `all` (the default); `-out` is required. In code this is `Manifest.Pack` and `Manifest.WritePack`.
