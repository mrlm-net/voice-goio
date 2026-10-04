package voices

import (
	"os"
	"path/filepath"

	voicegoio "github.com/mrlm-net/voice-goio"
	"github.com/mrlm-net/voice-goio/internal/userdir"
)

// Dir is the per-user folder voice models are downloaded into and read from
// when no other folder is given (PoolOptions.Dir, speaker.Options.VoicesDir):
//
//	%LOCALAPPDATA%\voice-goio\voices                on Windows
//	~/Library/Application Support/voice-goio/voices on macOS
//	$XDG_DATA_HOME/voice-goio/voices                elsewhere
//
// It is never the working directory. Nothing is created: the folder may not
// exist until the first download.
func Dir() string { return userdir.Voices() }

// Installed lists the manifest's models whose .onnx file is in dir ("" Dir()),
// in manifest order: the voices an application can offer its player (a crew
// voice picker), each with its speakers (Speakers, or SpeakerCount for a
// model not yet audited). Nothing is excluded: ExcludedVoice and licence
// filtering are the pool's, not this list's.
func Installed(m *Manifest, dir string) []Model {
	if dir == "" {
		dir = Dir()
	}
	var out []Model
	for _, mod := range m.Models {
		if isInstalled(dir, mod) {
			out = append(out, mod)
		}
	}
	return out
}

// isInstalled reports whether mod's .onnx file is under dir at the path the
// manifest gives it.
func isInstalled(dir string, mod Model) bool {
	_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(mod.ONNX)))
	return err == nil
}

// Profile is the voice of speakerID in the model, for an explicit voice
// (speaker.Utterance.Voice): the model, the speaker and its accent (the
// speaker's, else the model's). Prosody and Radio are left zero: piper's
// defaults, and the radio profile of the position it is said from.
func (m Model) Profile(speakerID int) voicegoio.VoiceProfile {
	v := voicegoio.VoiceProfile{Model: m.Name, SpeakerID: speakerID, Accent: m.Accent}
	for _, s := range m.Speakers {
		if s.ID == speakerID && s.Accent != "" {
			v.Accent = s.Accent
		}
	}
	return v
}
