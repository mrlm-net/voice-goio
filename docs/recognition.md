---
title: Recognition
description: Pilot or controller speech in, intent tags out — SAPI on Windows, the fake backend elsewhere, and grammars of your own.
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

`grammar.Words(s)` is the word split both use: lower case words, punctuation dropped, apostrophes kept (`"Doors closed."` is `doors`, `closed`).

A hand-written SRGS file works too (`Options.GrammarPath`): its public root rule must be `transmission`, its tags set `out.intent`, and a `callsign` rule is what `SetCallsigns` rebuilds.

## The controller side

For a game where the player is the controller, `grammar.Controller` recognises TWR and GND instructions. Its intents are the traffic engine's clearance verbs, so they map to actions without a table:

| Intent | Said | `value` | Further tags |
|---|---|---|---|
| `startup` | "start up approved, QNH 1013" | | `qnh` |
| `pushback` | "pushback approved, facing west" | facing | |
| `pushstart` | "push back and start up approved" | facing | |
| `taxi` | "taxi to holding point A1 runway 24 via B A" | runway | `holding_point`, `stand`, `via` |
| `upto` | "taxi via B, up to A1" / "hold short of C" | taxiway | `via` |
| `hold` | "hold short of runway 12" / "of C" / "hold position" | runway or taxiway | |
| `cross` | "cross runway 12" | runway | |
| `handoff` | "contact tower 118.1" | frequency | `station` |
| `lineup`, `lineupbehind` | "(behind the landing A320,) line up and wait runway 24" | runway | |
| `takeoff`, `land` | "runway 24 cleared for take-off, wind 240 degrees 8 knots" | runway | `wind` (`240/8`, `calm`) |
| `abort` | "stop immediately" / "cancel take-off clearance" | | |
| `goaround` | "go around" | | |
| `vacate` | "vacate left via L" | taxiway | `side` |

`say_again` and `standby` are recognised too. The callsign leads (or ends) the transmission and comes from the same dynamic rule.

```go
rec, err := sapi.New(sapi.Options{Grammar: grammar.Controller})
p := fake.NewControllerParser()        // the pure Go counterpart
r.UseControllerGrammar()               // a fake.Recognizer, roster kept
```

`voicecheck recog -grammar controller` runs `testdata/stt/controller.jsonl` through either backend.

## Distress and urgency

`MAYDAY` and `PAN PAN` are recognised by both backends and outrank every other intent, including `say_again`: a transmission containing "mayday" is a mayday whatever else is in it. The callsign is matched *anywhere* in the transmission, because a distress call puts the signal first, and it is reported confidently even when the aircraft is not on the roster — an aircraft in trouble may have just arrived on the frequency. `Tags[value]` carries the nature (`engine_failure`, `low_fuel`, `medical`, …) on a best effort basis; an unrecognised nature never downgrades the intent.

## Typing shortcuts

As a typing convenience, `stt/fake` also accepts the identifier and plain digits: `CSA1234 with you, 15000` and `DLH4EK request climb FL370` tag exactly as their spoken equivalents do. Speech never produces those forms, so nothing in the Windows backend has to cope with them.
