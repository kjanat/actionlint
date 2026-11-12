package cli

import (
	"fmt"
	"os"
)

// directoryOption implements Git-style sequential -C changes at the CLI boundary.
// Command restores the original process directory on every return path.
type directoryOption struct {
	original string
	current  string
}

func (d *directoryOption) Set(path string) error {
	if path == "" {
		return nil // Like git -C "", an empty argument leaves the directory alone.
	}
	if d.original == "" {
		original, err := os.Getwd()
		if err != nil {
			return err
		}
		d.original = original
	}
	if err := os.Chdir(path); err != nil {
		return fmt.Errorf("cannot change directory to %q: %w", path, err)
	}
	d.current = path
	return nil
}

func (d *directoryOption) String() string { return d.current }
func (*directoryOption) Type() string     { return "DIR" }
