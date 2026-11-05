// Package genfile formats generated files before publishing their contents.
package genfile

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Write uses the current directory's dprint configuration and the destination's
// filename to format content. Exclusive writes preserve versioned snapshots.
func Write(ctx context.Context, path string, content []byte, exclusive bool) error {
	if exclusive {
		if _, err := os.Lstat(path); err == nil {
			return &os.PathError{Op: "open", Path: path, Err: os.ErrExist}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	command := exec.CommandContext(ctx, "dprint", "fmt", "--stdin", filepath.Base(path))
	command.Stdin = bytes.NewReader(content)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	formatted, err := command.Output()
	if err != nil {
		return fmt.Errorf("format generated %s: %w: %s", path, err, &stderr)
	}
	if !exclusive {
		return os.WriteFile(path, formatted, 0644)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return writeNewFile(path, formatted)
}
