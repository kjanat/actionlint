package fsread

import (
	"os"
	"path/filepath"
)

// PhysicalPath resolves symlinks, including ancestors of files not yet on disk.
func PhysicalPath(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	parent, suffix := path, ""
	for {
		if resolved, err := filepath.EvalSymlinks(parent); err == nil {
			return filepath.Join(resolved, suffix)
		} else if !os.IsNotExist(err) {
			return path
		}
		next := filepath.Dir(parent)
		if next == parent {
			return path
		}
		suffix = filepath.Join(filepath.Base(parent), suffix)
		parent = next
	}
}
