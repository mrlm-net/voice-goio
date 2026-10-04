package voices

import (
	"context"
	"fmt"
)

// PackSize is the download size of pack in bytes (Manifest.Pack), for an
// application's "download 702 MB?" prompt.
func (m *Manifest) PackSize(pack string) (int64, error) {
	models, err := m.Pack(pack)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, mod := range models {
		if mod.Size <= 0 {
			return 0, fmt.Errorf("voices: %s has no recorded size", mod.Name)
		}
		total += mod.Size
	}
	return total, nil
}

// InstallPack downloads pack's models of the default manifest into dir
// ("" Dir()), each checked against its SHA-256, resuming a broken download
// and skipping a model already there and valid; progress (nil: none) gets
// the bytes done of the pack's total and the model being fetched. It is
// safe to run again: what is installed stays.
func InstallPack(ctx context.Context, pack, dir string, progress func(done, total int64, model string)) error {
	m, err := LoadDefault()
	if err != nil {
		return err
	}
	if dir == "" {
		dir = Dir()
	}
	models, err := m.Pack(pack)
	if err != nil {
		return err
	}
	total, err := m.PackSize(pack)
	if err != nil {
		return err
	}
	var done int64
	for i := range models {
		mod := models[i]
		before := done
		d := &Downloader{Dir: dir}
		if progress != nil {
			d.OnProgress = func(p Progress) { progress(before+p.Downloaded, total, mod.Name) }
			progress(done, total, mod.Name)
		}
		if _, err := d.Fetch(ctx, &mod); err != nil {
			return fmt.Errorf("voices: install %s: %w", mod.Name, err)
		}
		done += mod.Size
		if progress != nil {
			progress(done, total, mod.Name)
		}
	}
	return nil
}
