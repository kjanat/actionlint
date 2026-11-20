// Package genfile formats generated files before publishing their contents.
package genfile

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Write uses the current directory's kprint configuration and the destination's
// filename to format content. Exclusive writes preserve versioned snapshots.
func Write(ctx context.Context, path string, content []byte, exclusive bool) error {
	if exclusive {
		if _, err := os.Lstat(path); err == nil {
			return &os.PathError{Op: "open", Path: path, Err: os.ErrExist}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "kprint", "fmt", "--stdin", filepath.Base(path))
	command.WaitDelay = 5 * time.Second
	command.Stdin = bytes.NewReader(content)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	formatted, err := command.Output()
	if ctx.Err() != nil {
		return fmt.Errorf("format generated %s: %w", path, ctx.Err())
	}
	if err != nil {
		return fmt.Errorf("format generated %s: %w: %s", path, err, &stderr)
	}
	if !exclusive {
		return os.WriteFile(path, formatted, 0o644)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeNewFile(path, formatted)
}
