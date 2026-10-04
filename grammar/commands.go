package grammar

import (
	"encoding/xml"
	"fmt"
	"strings"
	"unicode"
)

// Command is one thing the player may say and the intent it is reported as
// (Recognition.Tags["intent"]): "gear_up" for "gear up" or "landing gear
// up".
type Command struct {
	Intent  string
	Phrases []string
}

// Commands is an application's own grammar: an SRGS document for
// sapi.Options.Grammar recognising exactly these phrases ("request taxi",
// "gear up", "doors closed"), each tagged with its command's intent. The root
// rule is "transmission" (activated while push to talk is held), optionally
// preceded by the dynamic "callsign" rule, so SetCallsigns works as with the
// ATC grammar and tags the callsign when one is said first.
//
// A phrase is said as its Words (case and punctuation do not matter). An
// intent is letters, digits, '_', '-' and '.'; an intent otherwise or a
// phrase without words is an error rather than a grammar SAPI refuses at run
// time.
func Commands(cmds []Command) (string, error) {
	if len(cmds) == 0 {
		return "", fmt.Errorf("grammar: no commands")
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<grammar version="1.0" xml:lang="en-US" mode="voice" root="transmission"
         tag-format="semantics/1.0"
         xmlns="http://www.w3.org/2001/06/grammar">
  <rule id="transmission" scope="public">
    <item repeat="0-1"><ruleref uri="#callsign"/><tag>out.callsign=rules.callsign;</tag></item>
    <one-of>
`)
	for _, c := range cmds {
		if !validIntent(c.Intent) {
			return "", fmt.Errorf("grammar: intent %q: letters, digits, _ - . only", c.Intent)
		}
		if len(c.Phrases) == 0 {
			return "", fmt.Errorf("grammar: intent %q has no phrases", c.Intent)
		}
		for _, p := range c.Phrases {
			words := Words(p)
			if len(words) == 0 {
				return "", fmt.Errorf("grammar: intent %q: phrase %q has no words", c.Intent, p)
			}
			b.WriteString("      <item>")
			_ = xml.EscapeText(&b, []byte(strings.Join(words, " "))) // a strings.Builder does not fail
			fmt.Fprintf(&b, `<tag>out.intent="%s";</tag></item>`+"\n", c.Intent)
		}
	}
	b.WriteString(`    </one-of>
  </rule>

  <!-- Rebuilt at runtime by SetCallsigns. Present so the grammar compiles standalone. -->
  <rule id="callsign" scope="public">
    <one-of>
      <item>unknown traffic<tag>out="UNKNOWN";</tag></item>
    </one-of>
  </rule>
</grammar>
`)
	return b.String(), nil
}

// Match is the intent of the command text says, compared word by word
// without case or punctuation: the typed or scripted counterpart of
// recognising with Commands' grammar (no callsign).
func Match(cmds []Command, text string) (intent string, ok bool) {
	said := strings.Join(Words(text), " ")
	if said == "" {
		return "", false
	}
	for _, c := range cmds {
		for _, p := range c.Phrases {
			if strings.Join(Words(p), " ") == said {
				return c.Intent, true
			}
		}
	}
	return "", false
}

// Words splits a phrase into lower case words, dropping punctuation but
// keeping apostrophes ("Doors closed." is "doors", "closed").
func Words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\''
	})
}

func validIntent(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}
