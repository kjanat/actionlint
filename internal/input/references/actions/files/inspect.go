// Package files acquires filesystem facts for action execution files.
package files

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	inputfiles "actionlint.kjanat.dev/v2/internal/input/files"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
)

// Inspect records missing and present paths. Unrepresentable or otherwise
// unchecked paths are unknown. Cancellation and access denial stop acquisition.
func Inspect(ctx context.Context, meta *actionmeta.Metadata, reader *inputfiles.Reader) (actionmeta.ReferencedFiles, error) {
	if reader == nil {
		reader = inputfiles.New(ctx, nil, nil)
	}
	availability := make(map[string]actionmeta.FileAvailability)
	for _, file := range meta.Runs.FileReferences() {
		if _, checked := availability[file]; checked {
			continue
		}
		availability[file] = actionmeta.FileUnknown
		if err := ctx.Err(); err != nil {
			return actionmeta.ReferencedFiles{}, err
		}
		path := filepath.Join(meta.Dir(), filepath.FromSlash(file))
		exists, err := reader.PathExists(path)
		switch {
		case err == nil && exists:
			availability[file] = actionmeta.FilePresent
		case err == nil:
			availability[file] = actionmeta.FileMissing
		case errors.Is(err, os.ErrPermission), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return actionmeta.ReferencedFiles{}, fmt.Errorf("inspect action execution file %q: %w", path, err)
		}
	}
	return actionmeta.NewReferencedFiles(availability), ctx.Err()
}
