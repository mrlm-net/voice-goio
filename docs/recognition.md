---
title: Recognition
description: Pilot speech in, intent tags out — SAPI on Windows, the fake backend elsewhere, and grammars of your own.
order: 14
section: guides
---

> `stt/sapi` is written but **has never run against a live engine**, and no current consumer uses it. See [Platform status](platform-status.md).

Recognition, on Windows:

```go
rec, err := sapi.New(sapi.Options{MinConfidence: 0.5})
rec.SetCallsigns(aircraftOnFrequency)   // on every roster change
rec.Start()                             // PTT pressed
rec.Stop()                              // PTT released
r := <-rec.Results()                    // exactly one result per cycle
switch r.Tags[voicegoio.TagIntent] { ... }
```

On macOS use `stt/fake` (typed input or a script) — the same interface.

## The dynamic callsign rule

Restricting recognition to the aircraft actually on frequency is worth more than any acoustic tuning. `SetCallsigns` clears and rebuilds the grammar's `callsign` rule, adding one word transition per phraseology variant, so a pilot may say "tree" or "three" for the same aircraft. `normalise.SpokenVariants(spoken)` gives every phraseology variant of a spoken callsign, for a recogniser's callsign list.

## Grammars of your own

The ATC grammar is the default, not the only one. An application listening for its own phrases (cockpit commands: "request taxi", "gear up", "doors closed") builds a grammar from them and hands it to SAPI; each phrase comes back as its intent. `grammar.Match` is the same matching for typed input and away from Windows.

```go
cmds := []grammar.Command{
    {Intent: "gear_up", Phrases: []string{"gear up", "landing gear up"}},
    {Intent: "doors_closed", Phrases: []string{"doors closed"}},
}
g, _ := grammar.Commands(cmds)
rec, err := sapi.New(sapi.Options{Grammar: g}) // r.Tags["intent"] == "gear_up"
intent, ok := grammar.Match(cmds, "Gear up")  // typed: "gear_up", true
```

A hand-written SRGS file works too (`Options.GrammarPath`): its public root rule must be `transmission`, its tags set `out.intent`, and a `callsign` rule is what `SetCallsigns` rebuilds.

## Distress and urgency

`MAYDAY` and `PAN PAN` are recognised by both backends and outrank every other intent, including `say_again`: a transmission containing "mayday" is a mayday whatever else is in it. The callsign is matched *anywhere* in the transmission, because a distress call puts the signal first, and it is reported confidently even when the aircraft is not on the roster — an aircraft in trouble may have just arrived on the frequency. `Tags[value]` carries the nature (`engine_failure`, `low_fuel`, `medical`, …) on a best effort basis; an unrecognised nature never downgrades the intent.

## Typing shortcuts

As a typing convenience, `stt/fake` also accepts the identifier and plain digits: `CSA1234 with you, 15000` and `DLH4EK request climb FL370` tag exactly as their spoken equivalents do. Speech never produces those forms, so nothing in the Windows backend has to cope with them.
