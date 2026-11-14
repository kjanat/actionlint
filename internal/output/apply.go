package output

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/files"
	sourceid "actionlint.kjanat.dev/v2/internal/input/source/identity"
	"actionlint.kjanat.dev/v2/internal/results/edits"
)

// ApplyEdits prepares all replacement files and verifies every revision before the
// first rename. Renames are atomic per file, not across a multi-file transaction.
// The returned paths identify completed writes even when a later write fails.
func ApplyEdits(ctx context.Context, p edits.Plan, reader *files.Reader) (written []string, err error) {
	p.Changes = slices.Clone(p.Changes)
	seen := map[string]bool{}
	for i := range p.Changes {
		path, err := filepath.EvalSymlinks(p.Changes[i].Path)
		if err != nil {
			return nil, err
		}
		if seen[path] {
			return nil, fmt.Errorf("multiple fix plans target %s", path)
		}
		seen[path] = true
		p.Changes[i].Path = path
	}
	temps := make([]string, len(p.Changes))
	defer func() {
		for _, path := range temps {
			if path != "" {
				_ = os.Remove(path)
			}
		}
	}()
	validate := func(c edits.Change) error {
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
