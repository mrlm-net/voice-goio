package voices

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Voice packs (#17): the sets of models shipped together, e.g. by an
// application's installer.
const (
	// PackCore is the default an installer ships: the English voices the
	// pool mostly uses (coreEnglish) and the controller accent models.
	PackCore = "core"
	// PackEnglish is every English model: controllers and crews without
	// accents.
	PackEnglish = "en"
	// PackAll is every model: the English ones and the accent models
	// (controllers at Czech, German, Dutch, Polish, French, Italian and
	// Spanish airports speak English with a light accent).
	PackAll = "all"
)

// Packs are the packs' names.
func Packs() []string { return []string{PackCore, PackEnglish, PackAll} }

// core are the models of PackCore: the English voices the pool mostly
// uses and one accent model per country (German: the 20-speaker MLS).
var core = []string{"en_GB-alan-medium", "en_GB-vctk-medium", "en_US-lessac-medium", "en_US-ryan-medium",
	"cs_CZ-jirka-medium", "de_DE-mls-medium", "fr_FR-mls-medium", "nl_NL-mls-medium", "pl_PL-mls-medium", "it_IT-riccardo-x_low", "es_ES-mls-medium"}

// Pack is the models of pack name in manifest order.
func (m *Manifest) Pack(name string) ([]Model, error) {
	switch name {
	case PackAll:
		return append([]Model(nil), m.Models...), nil
	case PackCore:
		var out []Model
		for _, mod := range m.Models {
			if slices.Contains(core, mod.Name) {
				out = append(out, mod)
			}
		}
		return out, nil
	case PackEnglish:
		var out []Model
		for _, mod := range m.Models {
			if strings.HasPrefix(mod.Name, "en_") {
				out = append(out, mod)
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("voices: no pack %q (%s)", name, strings.Join(Packs(), ", "))
}

// WritePack writes models, as installed in dir ("" Dir()), as a zip an
// installer unpacks into the voices folder: each model's onnx and config at
// their manifest paths, and voices.json, a manifest of just these models
// (with the full manifest's regions). A model not installed is an error:
// a pack is whole or not made.
func (m *Manifest) WritePack(w io.Writer, models []Model, dir string) error {
	if dir == "" {
		dir = Dir()
	}
	z := zip.NewWriter(w)
	for _, mod := range models {
		for _, rel := range []string{mod.ONNX, mod.Config} {
			if err := addFile(z, filepath.Join(dir, filepath.FromSlash(rel)), rel); err != nil {
				return fmt.Errorf("voices: pack %s: %w", mod.Name, err)
			}
		}
	}
	sub := Manifest{Models: models, Regions: m.Regions}
	b, err := json.MarshalIndent(sub, "", "  ")
	if err != nil {
		return err
	}
	f, err := z.Create("voices.json")
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		return err
	}
	return z.Close()
}

func addFile(z *zip.Writer, path, name string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := z.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	return err
}

// AccentModels are the models that read English through another
// language's voice (an espeak override): the controller accents.
func AccentModels(m *Manifest) []string {
	var out []string
	for _, mod := range m.Models {
		if mod.EspeakOverride != "" {
			out = append(out, mod.Name)
		}
	}
	return out
}
