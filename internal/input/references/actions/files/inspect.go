// Package files acquires filesystem facts for action execution files.
package files

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
)

// Inspect records missing and present paths. Unrepresentable or otherwise
// unchecked paths are unknown. Cancellation and access denial stop acquisition.
func Inspect(ctx context.Context, meta *actionmeta.Metadata) (actionmeta.ReferencedFiles, error) {
	availability := make(map[string]actionmeta.FileAvailability)
	for _, file := range meta.Runs.FileReferences() {
		if err := ctx.Err(); err != nil {
			return actionmeta.ReferencedFiles{}, err
		}
		path := filepath.Join(meta.Dir(), filepath.FromSlash(file))
		_, err := os.Stat(path)
		switch {
		case err == nil:
			availability[file] = actionmeta.FilePresent
		case errors.Is(err, os.ErrNotExist):
			availability[file] = actionmeta.FileMissing
		case errors.Is(err, os.ErrPermission):
			return actionmeta.ReferencedFiles{}, fmt.Errorf("inspect action execution file %q: %w", path, err)
		}
	}
	return actionmeta.NewReferencedFiles(availability), ctx.Err()
}
