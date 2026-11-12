package process

import (
	"context"
	"errors"
	"os"

	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

// materializeProgram writes exact verified source into a new private directory.
// Nothing is shared between invocations; cleanup also handles Windows read-only
// attributes after the interpreter has terminated.
func materializeProgram(ctx context.Context, program manifests.InlineProgram) (path string, cleanup func() error, err error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	directory, err := os.MkdirTemp("", "actionlint-program-")
	if err != nil {
		return "", nil, err
	}
	// Keep the declared filename as a suffix, including interpreter extensions.
	// CreateTemp atomically creates a new file with mode 0600.
	file, err := os.CreateTemp(directory, "*-"+program.Filename)
	if err != nil {
		return "", nil, errors.Join(err, os.Remove(directory))
	}
	path = file.Name()
	cleanup = func() error {
		var failure error
		if err := os.Chmod(path, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
			failure = err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			failure = errors.Join(failure, err)
		}
		if err := os.Remove(directory); err != nil && !errors.Is(err, os.ErrNotExist) {
			failure = errors.Join(failure, err)
		}
		return failure
	}
	_, writeErr := file.WriteString(program.Inline)
	if err = errors.Join(writeErr, file.Close()); err != nil {
		return "", nil, errors.Join(err, cleanup())
	}
	if err = os.Chmod(path, 0o400); err != nil {
		return "", nil, errors.Join(err, cleanup())
	}
	if err = ctx.Err(); err != nil {
		return "", nil, errors.Join(err, cleanup())
	}
	return path, cleanup, nil
}
