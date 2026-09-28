// Package say is a development only TTS backend built on the macOS `say`
// command.
//
// SPEC.md develops on macOS and ships on Windows, where piper is the only
// synthesiser. But piper needs a downloaded binary and several hundred
// megabytes of voice models before anything can be heard at all, which makes
// the first end to end run of the pipeline needlessly expensive. `say` is
// already on every Mac, runs entirely offline, is reached as a subprocess (no
// cgo, no module dependency), and ships genuinely different accents: en_GB,
// en_AU, en_IE, en_IN, en_ZA plus the non English voices that produce the same
// "non native controller" effect as piper's espeak swap trick.
//
// It is not a shipping backend. On Windows New returns
// voicegoio.ErrNotImplemented and the application uses tts/piper.
package say

import (
	"strings"

	voicegoio "github.com/mrlm-net/voice-goio"
)

// Options configures the backend.
type Options struct {
	// Rate is the base speaking rate in words per minute; 0 means 165.
	//
	// Controllers do talk fast, but these are not neural voices: pushed past
	// about 190 wpm they smear, and through a 3 kHz band the result is hard to
	// follow. VoiceProfile.LengthScale still scales it, so a center controller
	// is audibly quicker than ground.
	Rate int
	// SampleRate is the rate `say` is asked to render at. 0 means 22050, which
	// matches piper's "medium" voices so the radio chain behaves identically.
	SampleRate int
	// Voice forces one macOS voice for every profile, ignoring the mapping.
	Voice string
}

// Voice is one macOS voice with the accent it stands in for.
type Voice struct {
	Name   string
	Accent string
	Kind   string // "native" or "non-native"
}

// Catalogue is the curated set used when a VoiceProfile names a piper model
// that has no explicit mapping. The order is stable, so assignment by speaker
// index is deterministic across runs.
var Catalogue = []Voice{
	{"Daniel", "en-GB", "native"},
	{"Karen", "en-AU", "native"},
	{"Moira", "en-IE", "native"},
	{"Rishi", "en-IN", "native"},
	{"Tessa", "en-ZA", "native"},
	{"Samantha", "en-US", "native"},
	{"Ralph", "en-US", "native"},
	{"Kathy", "en-US", "native"},
	{"Albert", "en-US", "native"},
	{"Anna", "en-DE", "non-native"},
	{"Alice", "en-IT", "non-native"},
	{"Ellen", "en-NL", "non-native"},
	{"Zosia", "en-PL", "non-native"},
	{"Zuzana", "en-CZ", "non-native"},
	{"Fred", "en-ATIS", "machine"},
	{"Thomas", "en-FR", "non-native"},
	{"Monica", "en-ES", "non-native"},
}

// modelVoice maps the piper models named in SPEC.md 4.3 onto the closest macOS
// voice, so the same VoiceProfile can be used on both platforms and only the
// backend changes.
var modelVoice = map[string]string{
	"en_GB-vctk-medium":             "Daniel",
	"en_GB-alan-medium":             "Daniel",
	"en_GB-alba-medium":             "Moira",
	"en_GB-aru-medium":              "Daniel",
	"en_GB-northern_english_male":   "Daniel",
	"en_GB-southern_english_female": "Karen",
	"en_US-libritts_r-medium":       "Samantha",
	"en_US-arctic-medium":           "Fred",
	"en_US-l2arctic-medium":         "Rishi",
	"en_US-ryan-medium":             "Ralph",
	"en_US-joe-medium":              "Fred",
	"en_US-lessac-medium":           "Samantha",
	"en_US-hfc_male-medium":         "Albert",
	"en_US-hfc_female-medium":       "Kathy",
	"de_DE-thorsten-medium":         "Anna",
	"de_DE-mls-medium":              "Anna",
	"nl_NL-mls-medium":              "Ellen",
	"pl_PL-mls-medium":              "Zosia",
	"fr_FR-mls-medium":              "Thomas",
	"cs_CZ-jirka-medium":            "Zuzana",
	"it_IT-riccardo-medium":         "Alice",
	"es_ES-mls-medium":              "Monica",
}

// accentFamilies groups the catalogue by the accent family a piper model
// belongs to, so a multi speaker model maps onto voices that actually belong to
// its region.
//
// The families follow what each model actually contains rather than what its
// name suggests. en_GB-vctk-medium is the clearest case: VCTK documents its
// 109 speakers as English, Scottish, Irish, Welsh, Australian, New Zealand,
// Indian, South African and Canadian, so its family spans the Commonwealth and
// an Indian accented controller at a British airport is correct, not a bug.
// What was wrong before was mapping its speakers across *every* accent,
// including German and Italian, which no VCTK speaker is.
var accentFamilies = map[string][]string{
	"en-GB": {"Daniel", "Moira", "Rishi", "Karen", "Tessa"},
	"en-US": {"Samantha", "Ralph", "Kathy", "Albert"},
	"en-L2": {"Rishi", "Anna", "Alice", "Monica"},
	"en-DE": {"Anna"},
	"en-NL": {"Ellen"},
	"en-PL": {"Zosia"},
	"en-CZ": {"Zuzana"},
	"en-FR": {"Thomas"},
	"en-IT": {"Alice"},
	"en-ES": {"Monica"},
}

// accentVoice maps an accent label from the voice manifest onto the macOS
// voice that stands in for it.
var accentVoice = map[string]string{
	"en-GB": "Daniel", "en-GB-north": "Daniel", "en-GB-south": "Karen",
	"en-GB-scottish": "Moira", "en-IE": "Moira",
	"en-US": "Samantha", "en-CA": "Ralph",
	"en-AU": "Karen", "en-NZ": "Karen",
	"en-IN": "Rishi", "en-ZA": "Tessa", "en-L2": "Rishi",
	"en-DE": "Anna", "en-NL": "Ellen", "en-PL": "Zosia", "en-CZ": "Zuzana",
	"en-FR": "Thomas", "en-IT": "Alice", "en-ES": "Monica",
	"en-CN": "Alice", "en-AR": "Monica", "en-KO": "Rishi", "en-VI": "Rishi",
	// An ATIS is a machine reading a template on a loop. Fred is the flattest,
	// most synthetic voice macOS ships, which is exactly right for it.
	"en-ATIS": "Fred",
}

// family maps a piper model name onto its accent family key.
func family(model string) string {
	switch {
	case strings.HasPrefix(model, "en_GB"):
		return "en-GB"
	case strings.HasPrefix(model, "en_US-l2arctic"):
		return "en-L2"
	case strings.HasPrefix(model, "en_US"):
		return "en-US"
	case strings.HasPrefix(model, "de_DE"):
		return "en-DE"
	case strings.HasPrefix(model, "nl_NL"):
		return "en-NL"
	case strings.HasPrefix(model, "pl_PL"):
		return "en-PL"
	case strings.HasPrefix(model, "cs_CZ"):
		return "en-CZ"
	case strings.HasPrefix(model, "fr_FR"):
		return "en-FR"
	case strings.HasPrefix(model, "it_IT"):
		return "en-IT"
	case strings.HasPrefix(model, "es_ES"):
		return "en-ES"
	}
	return ""
}

// voiceFor picks the macOS voice for a profile: an explicit override, then the
// per model mapping for single speaker models, then the accent family of the
// model indexed by speaker so a multi speaker model still yields variety
// without leaving its region.
func voiceFor(opt Options, p voicegoio.VoiceProfile) string {
	if opt.Voice != "" {
		return opt.Voice
	}
	// An accent assigned by the voice pool is the most specific thing we know,
	// so it wins: a Delhi controller gets the Indian voice whichever piper
	// model the speaker happens to live in.
	if v, ok := accentVoice[p.Accent]; ok {
		return v
	}
	if p.SpeakerID == 0 {
		if v, ok := modelVoice[p.Model]; ok {
			return v
		}
	}
	if fam, ok := accentFamilies[family(p.Model)]; ok && len(fam) > 0 {
		return fam[p.SpeakerID%len(fam)]
	}
	if v, ok := modelVoice[p.Model]; ok {
		return v
	}
	return Catalogue[p.SpeakerID%len(Catalogue)].Name
}

// rateFor converts piper's length_scale into words per minute. length_scale is
// a duration multiplier, so speaking rate is its reciprocal.
func rateFor(opt Options, p voicegoio.VoiceProfile) int {
	base := opt.Rate
	if base <= 0 {
		base = 165
	}
	if p.LengthScale <= 0 {
		return base
	}
	r := int(float32(base) / p.LengthScale)
	return min(max(r, 90), 195)
}
