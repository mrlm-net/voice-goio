---
title: Design Notes
description: Three decisions worth knowing about, and where the library departs from the original spec.
order: 22
section: reference
---

## Three decisions worth knowing about

**Utterance framing.** In `--output-raw` mode piper writes the samples for a line and then waits. There is no length prefix and no end marker. The backend appends a fixed sentinel word after the real text, reads until stdout has been quiet for 150 ms, and cuts the sentinel's samples back off (its length is measured once per voice shape). If the trim is wrong, every transmission ends with a stray "dot" — `TestSynthesizeTrimsSentinel` is what guards it.

**The dynamic callsign rule.** Restricting recognition to the aircraft actually on frequency is worth more than any acoustic tuning. `SetCallsigns` clears and rebuilds the grammar's `callsign` rule, adding one word transition per phraseology variant, so a pilot may say "tree" or "three" for the same aircraft.

**Loudness, not peaks.** The radio chain compresses hard and normalises to −15 dBFS RMS under a −3 dBFS soft ceiling. Normalising peaks instead — which is what the spec's §4.6 asks for — leaves speech around −20 dBFS RMS because of its 15 dB crest factor, and it sounds distant and hard to follow. Compression is also what real transmitters do, and it is the single biggest intelligibility win in the chain. `internal/dsp` has the stage-by-stage level test.

## Known deviations from the spec

**Radio chain order (§4.6).** Levelling happens on the speech, before the noise floor and squelch bursts, rather than as the last stage before resampling, and it targets RMS with a soft ceiling rather than a peak. Two reasons: a long squelch tail otherwise drags the speech level down with it, and peak normalisation does not control how loud speech actually sounds. A side effect worth having is that the per-profile noise figures now mean something absolute — −45 dBFS of noise under speech at −15 dBFS RMS is a 30 dB signal-to-noise ratio on every profile, whatever the voice did.

**Phraseology (§4.1).** The [spec](SPEC.md) §4.1 shows `127.45` spoken as `one two seven decimal four five` under ICAO, which contradicts the digit table two rows above it in the same table (ICAO speaks 5 as "fife", as the `FL350` and `SQK 4321` rows do). The digit table is the general rule, so it wins: the frequency reads `one two seven decimal four fife`. If the literal spec text is wanted instead, that is a one-line change in `normalise.digits` and one line in `testdata/normalise/icao.txt`.

## Versions describe what changed

Versions describe what changed, not what is planned. The spec's §5 earmarked `v0.2.0` for the Windows bring-up; that number went to an earlier release because it added API, and pre-assigning numbers to milestones turned out to be a way of being wrong twice. The bring-up will land in whatever version follows it.
