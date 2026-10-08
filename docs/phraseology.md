---
title: Phraseology and Prosody
description: How the normaliser turns written ATC text into spoken ICAO or FAA phraseology, with the pauses in the right places.
order: 15
section: guides
---

`normalise` converts ATC message text written the way the application holds it into what a controller says:

`BAW123 climb FL350` → `Speedbird one two tree, climb flight level tree fife zero`

It covers ICAO and FAA phraseology (`voicegoio.ICAO`, `voicegoio.FAA`), emergency signals, METAR shorthand and the pauses.

## Pauses are the normaliser's job

Both piper (through espeak) and macOS `say` take their pauses from punctuation, so inserting commas at semantic boundaries is the only lever there is. The normaliser adds one after a callsign, between the elements of a taxi route (`TWY A3 and B` → "taxiway alpha tree, bravo"), after a distress signal, and between an information unit and the instruction that follows it. That last rule keys off having just finished a keyword group rather than off the verb alone, which is what stops `radar contact` becoming "radar, contact".

## Emergencies

The library pronounces what the application writes and does not rewrite phraseology, so the application sends the real thing — `MAYDAY MAYDAY MAYDAY` or `PAN PAN, PAN PAN, PAN PAN`. It does know that "mayday" is a one-word signal and "pan pan" a two-word one, so the pauses land between the repeats instead of inside them.

## METAR shorthand and the ATIS

The normaliser speaks METAR shorthand, so a broadcast can be written the way the application already holds it — `BKN020` → "broken two thousand", `10KM BR` → "one zero kilometres mist", `NOSIG` → "no significant change".

## Digits

ICAO speaks 5 as "fife", as the `FL350` and `SQK 4321` examples do, and the frequency `127.45` reads `one two seven decimal four fife`. The historic [design spec](SPEC.md) §4.1 shows "five" there, contradicting its own digit table; the digit table wins. See [Design notes](design-notes.md).
