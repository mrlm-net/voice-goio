// Package voices manages the piper voice pool: the manifest of models and
// speakers, downloading them on first run, and assigning a voice to a
// controller.
//
// The pool is what makes a sector sound populated rather than like one person
// reading a script. Two things matter: a controller keeps the same voice for a
// session, and two controllers at the same airport never share one. Both fall
// out of deterministic assignment from a session seed, which also makes a
// recorded session reproducible.
package voices

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/internal/userdir"
)

//go:embed voices.json
var defaultManifest []byte

// Speaker is one voice inside a model. Pass is set by a human during the audit
// pass (see cmd/voicecheck audit): a speaker that sounds wrong through the
// radio chain, or that the espeak swap trick mangles, is marked false and never
// assigned.
type Speaker struct {
	ID      int    `json:"id"`
	Label   string `json:"label"`
	Accent  string `json:"accent"`
	Quality int    `json:"quality"` // 1-5, set during the audit
	Pass    bool   `json:"pass"`
}

// Model is one .onnx/.onnx.json pair.
type Model struct {
	Name       string `json:"name"`
	ONNX       string `json:"onnx"`   // path relative to the repository root
	Config     string `json:"config"` // path relative to the repository root
	SHA256     string `json:"sha256"`
	SampleRate int    `json:"sample_rate"`
	License    string `json:"license"`
	// Accent is the model's primary accent.
	Accent string `json:"accent"`
	// Accents is every accent the model's speakers are documented to cover.
	// en_GB-vctk-medium is the reason this exists: VCTK's 109 speakers span
	// the British Isles, Australia, New Zealand, India, South Africa and
	// Canada, and a manifest that calls the whole model "en-GB" leaves a
	// Sydney or a Delhi controller with no regional voice at all.
	Accents []string `json:"accents"`
	// SpeakerCount is how many speakers the model carries. Multi speaker
	// models such as en_US-libritts_r-medium have hundreds; listing every one
	// before anybody has listened to them would be fabricating audit results,
	// so they are assignable as unaudited candidates until the audit runner
	// records a curated entry in Speakers.
	SpeakerCount int `json:"speaker_count"`
	// EspeakOverride applies the swap trick: a copy of the model config with
	// its phonemizer forced to an English voice, so a German or Czech model
	// reads English text with that accent. Never the other way round.
	EspeakOverride string    `json:"espeak_override"`
	Speakers       []Speaker `json:"speakers"`
}

// Manifest is voices.json.
type Manifest struct {
	Models []Model `json:"models"`
	// Regions maps an ICAO location prefix to the accents that belong there,
	// most likely first: "LK" (Czechia) gets Czech, British and German accented
	// English before anything else.
	Regions map[string][]string `json:"regions"`
}

// LoadDefault returns the manifest embedded in the library.
func LoadDefault() (*Manifest, error) { return Parse(defaultManifest) }

// Load reads a manifest from disk.
func Load(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// Parse validates and returns a manifest.
func Parse(b []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("voices: parse manifest: %w", err)
	}
	if len(m.Models) == 0 {
		return nil, fmt.Errorf("voices: manifest has no models")
	}
	for i, mod := range m.Models {
		if mod.Name == "" || mod.ONNX == "" || mod.Config == "" {
			return nil, fmt.Errorf("voices: model %d is missing name, onnx or config", i)
		}
	}
	return &m, nil
}

// Save writes a manifest back, which is how the audit runner records pass
// flags and how the downloader records hashes it had to compute.
func (m *Manifest) Save(path string) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// Model looks a model up by name.
func (m *Manifest) Model(name string) (*Model, bool) {
	for i := range m.Models {
		if m.Models[i].Name == name {
			return &m.Models[i], true
		}
	}
	return nil, false
}

// permissiveLicences are the licences that survive commercial filtering. A
// model whose MODEL_CARD says anything else is excluded when the pool is built
// with CommercialOnly, which the commercial build tag turns on by default.
var permissiveLicences = map[string]bool{
	"CC-BY-4.0":     true,
	"CC-BY-SA-4.0":  true,
	"CC0-1.0":       true,
	"public-domain": true,
	"MIT":           true,
	"Apache-2.0":    true,
	"BSD-3-Clause":  true,
}

// PoolOptions configures assignment.
type PoolOptions struct {
	// Dir is where the model files live. Empty means the per user data
	// directory, never the current working directory.
	Dir string
	// Seed makes a session reproducible. 0 means a fixed default, so a run
	// with no explicit seed is still deterministic.
	Seed int64
	// CommercialOnly restricts assignment to permissively licensed models.
	// Defaults to the value of the commercial build tag.
	CommercialOnly bool
	// AllowUnaudited assigns speakers whose pass flag is still false. It is
	// what makes a freshly downloaded model usable before anyone has listened
	// to it.
	AllowUnaudited bool
}

// Pool assigns voices to controllers.
type Pool struct {
	man  *Manifest
	opt  PoolOptions
	mu   sync.Mutex
	used map[string]map[string]bool // airport -> "model#speaker" -> taken
	held map[string]voicegoio.VoiceProfile
	// have is the set of manifest models whose files are in Dir, looked up
	// once (haveOnce).
	have     map[string]bool
	haveOnce sync.Once
}

// installed reports whether model is downloaded into the pool's Dir. A
// voice that is not cannot be spoken: assigning it left an airport silent
// (the speaker logged "not found" and said nothing). When no model at all
// is installed, every model counts, so assignment stays usable (and
// reproducible) before the first download.
func (p *Pool) installed(model string) bool {
	p.haveOnce.Do(func() {
		p.have = map[string]bool{}
		for _, m := range p.man.Models {
			if _, err := os.Stat(filepath.Join(p.opt.Dir, filepath.FromSlash(m.ONNX))); err == nil {
				p.have[m.Name] = true
			}
		}
	})
	return len(p.have) == 0 || p.have[model]
}

// NewPool prepares assignment over a manifest.
func NewPool(m *Manifest, opt PoolOptions) *Pool {
	if opt.Dir == "" {
		// Without this, an empty Dir resolved to a relative path and the
		// downloader wrote models into whatever directory the tool was run
		// from. Sixty megabytes of model in a source tree is a bug, not a
		// default.
		opt.Dir = userdir.Voices()
	}
	if opt.Seed == 0 {
		opt.Seed = 0x5643474F // "VCGO"
	}
	if !opt.CommercialOnly {
		opt.CommercialOnly = commercialDefault
	}
	return &Pool{
		man:  m,
		opt:  opt,
		used: map[string]map[string]bool{},
		held: map[string]voicegoio.VoiceProfile{},
	}
}

// candidate is one assignable voice with its region score.
type candidate struct {
	model   string
	speaker int
	accent  string
	score   int
}

// accentsFor returns the preferred accents for an airport, longest prefix
// first: "LKPR" matches "LK", "KJFK" matches "K".
func (m *Manifest) accentsFor(icao string) []string {
	icao = strings.ToUpper(icao)
	best, bestLen := []string(nil), -1
	for prefix, accents := range m.Regions {
		if strings.HasPrefix(icao, strings.ToUpper(prefix)) && len(prefix) > bestLen {
			best, bestLen = accents, len(prefix)
		}
	}
	return best
}

// AtisAccent is the pseudo accent recorded on an ATIS voice profile. Backends
// that do not use piper models key off it to pick their flattest voice.
const AtisAccent = "en-ATIS"

// atisPreference is the order in which a model is chosen to be the ATIS voice.
// Single speaker, neutral, permissively licensed: an ATIS is a machine reading
// a template, and it should sound like one.
var atisPreference = []string{
	"en_US-lessac-medium", "en_US-ryan-medium", "en_GB-alan-medium", "en_US-hfc_male-medium",
}

// atisVoice returns the fixed ATIS profile.
//
// Unlike every other position, an ATIS is not assigned from the regional pool
// and does not consume one of an airport's voices. It is a recording played on
// a loop from a machine, so it is the same flat voice at every airport, and
// giving Gatwick a different ATIS reader from Heathrow would be wrong rather
// than varied. The prosody is deliberately at the slow, flat end of the range.
func (p *Pool) atisVoice() voicegoio.VoiceProfile {
	v := voicegoio.VoiceProfile{
		SpeakerID:   0,
		LengthScale: 0.95,
		NoiseScale:  0.50,
		NoiseW:      0.70,
		Radio:       string(voicegoio.ATIS),
		Accent:      AtisAccent,
	}
	for _, name := range atisPreference {
		if _, ok := p.man.Model(name); ok && p.installed(name) {
			v.Model = name
			return v
		}
	}
	for _, m := range p.man.Models {
		if p.installed(m.Name) {
			v.Model = m.Name
			break
		}
	}
	return v
}

// Assign picks a voice for a controller position at an airport.
//
// The same (airport, kind) always gets the same voice within a session, and a
// second position at the same airport never gets the same speaker: hearing the
// same voice on tower and ground breaks the illusion faster than any accent
// mismatch.
func (p *Pool) Assign(icaoPrefix string, kind voicegoio.ControllerKind) voicegoio.VoiceProfile {
	if kind == voicegoio.ATIS {
		return p.atisVoice()
	}
	key := strings.ToUpper(icaoPrefix) + "/" + string(kind)

	p.mu.Lock()
	defer p.mu.Unlock()
	if v, ok := p.held[key]; ok {
		return v
	}

	cands := p.candidates(icaoPrefix)
	airport := strings.ToUpper(icaoPrefix)
	taken := p.used[airport]
	if taken == nil {
		taken = map[string]bool{}
		p.used[airport] = taken
	}

	// Candidates are grouped into score tiers and searched tier by tier. The
	// deterministic offset varies the choice *within* a tier, so an airport
	// still gets a different voice per position without ever reaching past a
	// better regional match into a worse one.
	h := hash64(p.opt.Seed, key)
	var chosen candidate
	found := false
	for _, tier := range tiers(cands) {
		offset := int(h % uint64(len(tier)))
		for i := range tier {
			c := tier[(offset+i)%len(tier)]
			if !taken[fmt.Sprintf("%s#%d", c.model, c.speaker)] {
				chosen, found = c, true
				break
			}
		}
		if found {
			break
		}
	}
	if !found && len(cands) > 0 {
		// Every voice is already in use at this airport: reuse one rather than
		// fall back to silence.
		chosen, found = cands[int(h%uint64(len(cands)))], true
	}

	v := voicegoio.VoiceProfile{Radio: radioProfile(kind)}
	if found {
		taken[fmt.Sprintf("%s#%d", chosen.model, chosen.speaker)] = true
		v.Model = chosen.model
		v.SpeakerID = chosen.speaker
		v.Accent = chosen.accent
	}
	// Prosody per position: an en route controller working twelve aircraft
	// talks faster than a ground controller.
	v.LengthScale, v.NoiseScale, v.NoiseW = prosody(kind, hash64(p.opt.Seed, key))
	p.held[key] = v
	return v
}

// Release forgets an assignment, for when a controller position closes.
func (p *Pool) Release(icaoPrefix string, kind voicegoio.ControllerKind) {
	key := strings.ToUpper(icaoPrefix) + "/" + string(kind)
	p.mu.Lock()
	defer p.mu.Unlock()
	if v, ok := p.held[key]; ok {
		delete(p.held, key)
		if taken := p.used[strings.ToUpper(icaoPrefix)]; taken != nil {
			delete(taken, fmt.Sprintf("%s#%d", v.Model, v.SpeakerID))
		}
	}
}

// candidates lists the assignable voices for an airport, best region match
// first. The list is stable, which is what makes assignment reproducible.
func (p *Pool) candidates(icaoPrefix string) []candidate {
	prefs := p.man.accentsFor(icaoPrefix)
	rank := make(map[string]int, len(prefs))
	for i, a := range prefs {
		rank[strings.ToLower(a)] = len(prefs) - i
	}

	var out []candidate
	for _, m := range p.man.Models {
		if p.opt.CommercialOnly && !permissiveLicences[m.License] || !p.installed(m.Name) {
			continue
		}
		for _, s := range speakersOf(m, p.opt.AllowUnaudited) {
			if !s.Pass && !p.opt.AllowUnaudited {
				continue
			}
			score := rank[strings.ToLower(s.Accent)]
			if score == 0 {
				// Not a regional match: usable, but only after everything that
				// is. Quality still orders these against each other.
				score = -5 + s.Quality
			} else {
				score = score*10 + s.Quality
			}
			out = append(out, candidate{model: m.Name, speaker: s.ID, accent: s.Accent, score: score})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		if out[i].model != out[j].model {
			return out[i].model < out[j].model
		}
		return out[i].speaker < out[j].speaker
	})
	return out
}

// tiers splits the sorted candidate list into runs of equal score.
func tiers(cands []candidate) [][]candidate {
	var out [][]candidate
	for i := 0; i < len(cands); {
		j := i
		for j < len(cands) && cands[j].score == cands[i].score {
			j++
		}
		out = append(out, cands[i:j])
		i = j
	}
	return out
}

// Count reports how many voices are assignable, which is the number SPEC.md 9
// sets a floor of 100 on.
func (p *Pool) Count() int { return len(p.candidates("")) }

// Accents reports the distinct accents among assignable voices.
func (p *Pool) Accents() []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range p.candidates("") {
		if c.accent != "" && !seen[c.accent] {
			seen[c.accent] = true
			out = append(out, c.accent)
		}
	}
	sort.Strings(out)
	return out
}

// speakersOf returns the assignable speakers of a model: the curated list when
// the audit has produced one, otherwise placeholders derived from
// SpeakerCount, which are only offered when unaudited voices are allowed.
func speakersOf(m Model, allowUnaudited bool) []Speaker {
	if len(m.Speakers) > 0 {
		out := make([]Speaker, 0, len(m.Speakers))
		for _, s := range m.Speakers {
			if s.Accent == "" {
				s.Accent = m.Accent
			}
			out = append(out, s)
		}
		return out
	}
	if !allowUnaudited {
		return nil
	}
	n := max(m.SpeakerCount, 1)
	accents := m.Accents
	if len(accents) == 0 {
		accents = []string{m.Accent}
	}
	out := make([]Speaker, 0, n)
	for i := range n {
		// The declared accents are spread over the speakers round robin. This
		// is a placeholder, not a claim about any individual speaker: the
		// audit pass replaces it with a curated Speakers list.
		out = append(out, Speaker{ID: i, Label: fmt.Sprintf("s%d", i), Accent: accents[i%len(accents)]})
	}
	return out
}

func radioProfile(kind voicegoio.ControllerKind) string {
	switch kind {
	case voicegoio.Ground, voicegoio.Tower, voicegoio.Approach, voicegoio.Center, voicegoio.ATIS:
		return string(kind)
	default:
		return string(voicegoio.Tower)
	}
}

// prosody returns piper's length_scale, noise_scale and noise_w for a position,
// jittered per controller so two voices on the same model still differ.
func prosody(kind voicegoio.ControllerKind, h uint64) (length, noise, noiseW float32) {
	base := float32(0.92)
	switch kind {
	case voicegoio.Center:
		base = 0.85
	case voicegoio.Approach:
		base = 0.88
	case voicegoio.ATIS:
		base = 0.95
	}
	jitter := float32(h%7) / 100 // 0.00 - 0.06
	length = base + jitter - 0.03
	if length < 0.80 {
		length = 0.80
	}
	if length > 0.95 {
		length = 0.95
	}
	return length, 0.55 + float32(h/7%5)/100, 0.70 + float32(h/31%15)/100
}

func hash64(seed int64, s string) uint64 {
	h := fnv.New64a()
	fmt.Fprintf(h, "%d/%s", seed, s)
	return h.Sum64()
}

// ---- piper.Resolver --------------------------------------------------------

// Dir is where model files are stored.
func (p *Pool) Dir() string { return p.opt.Dir }

// Resolve implements piper.Resolver: it turns a model name into the file pair
// piper is launched with, applying the espeak override when the manifest
// declares one.
func (p *Pool) Resolve(model string) (string, string, error) {
	m, ok := p.man.Model(model)
	if !ok {
		return "", "", fmt.Errorf("voices: %q is not in the manifest", model)
	}
	onnx := filepath.Join(p.opt.Dir, filepath.FromSlash(m.ONNX))
	cfg := filepath.Join(p.opt.Dir, filepath.FromSlash(m.Config))
	if _, err := os.Stat(onnx); err != nil {
		return "", "", fmt.Errorf("voices: %q is not downloaded: %w", model, err)
	}
	if m.EspeakOverride == "" {
		return onnx, cfg, nil
	}
	over, err := p.writeOverride(m)
	if err != nil {
		return "", "", err
	}
	return onnx, over, nil
}

// writeOverride materialises the swap trick config: the model's own config with
// the phonemizer voice replaced.
//
// Phonemes the model was never trained on are simply dropped by piper (it warns
// on stderr), which is why this only ever runs English text through a non
// English model and never the reverse.
func (p *Pool) writeOverride(m *Model) (string, error) {
	src := filepath.Join(p.opt.Dir, filepath.FromSlash(m.Config))
	b, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("voices: read config for %s: %w", m.Name, err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		return "", fmt.Errorf("voices: parse config for %s: %w", m.Name, err)
	}
	cfg["espeak"] = map[string]any{"voice": m.EspeakOverride}

	dir := filepath.Join(p.opt.Dir, "_espeak_override")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, m.Name+".onnx.json")
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, out, 0o644); err != nil {
		return "", err
	}
	return dst, nil
}

// Seed reports the session seed, so a caller can derive its own deterministic
// values (the radio chain's noise, for instance) from the same session.
func (p *Pool) Seed() int64 { return p.opt.Seed }
