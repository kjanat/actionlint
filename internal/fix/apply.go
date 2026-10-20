package fix

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/sourceid"
)

// Apply prepares all replacement files and verifies every revision before the
// first rename. Renames are atomic per file, not across a multi-file transaction.
// The returned paths identify completed writes even when a later write fails.
func Apply(ctx context.Context, p Plan, reader *fsread.Reader) (written []string, err error) {
	temps := make([]string, len(p.Changes))
	defer func() {
		for _, path := range temps {
			if path != "" {
				_ = os.Remove(path)
			}
		}
	}()
	validate := func(c Change) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := os.Lstat(c.Path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("fix destination %s is not a regular file", c.Path)
		}
		data, err := reader.ReadFile(c.Path)
		if err != nil {
			return err
		}
		if sourceid.Digest(data) != c.Revision || string(data) != c.Before {
			return fmt.Errorf("source changed since fix preview: %s", c.Path)
		}
		return nil
	}
	for i, c := range p.Changes {
		if err := validate(c); err != nil {
			return nil, err
		}
		info, err := os.Stat(c.Path)
		if err != nil {
			return nil, err
		}
		file, err := os.CreateTemp(filepath.Dir(c.Path), ".actionlint-fix-*")
		if err != nil {
			return nil, err
		}
		temps[i] = file.Name()
		if err = file.Chmod(info.Mode().Perm()); err == nil {
			_, err = file.WriteString(c.After)
		}
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	for _, c := range p.Changes {
		if err := validate(c); err != nil {
			return nil, err
		}
	}
	for i, c := range p.Changes {
		if err := validate(c); err != nil {
			return written, err
		}
		if err := os.Rename(temps[i], c.Path); err != nil {
			return written, err
		}
		temps[i] = ""
		written = append(written, c.Path)
	}
	return written, nil
}
