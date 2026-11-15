// Package grammar embeds the SRGS grammar used by the Windows SAPI5 recogniser
// and builds an application's own (Commands).
//
// The same file is the reference for the pure Go tag parser in stt/fake, so the
// two backends stay in step: whatever atc.grxml can match, the fake parser must
// be able to tag identically. A Commands grammar's counterpart is Match.
package grammar

import _ "embed"

// ATC is the SRGS XML loaded with ISpRecoGrammar::LoadCmdFromMemory. The
// "callsign" rule is declared empty on purpose: it is rebuilt at runtime from
// the aircraft actually on frequency.
//
//go:embed atc.grxml
var ATC string

// Controller is the SRGS XML for the other side of the radio: the player as
// the controller (TWR and GND), its intents the traffic engine's clearance
// verbs (taxi, lineup, takeoff, land, ...). Its "callsign" rule is dynamic
// as in ATC. The pure Go counterpart is stt/fake's NewControllerParser.
//
//go:embed controller.grxml
var Controller string
