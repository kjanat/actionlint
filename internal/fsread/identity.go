package fsread

import (
	"os"
	"path/filepath"
)

// PhysicalPath also resolves aliases above an in-memory file which does
// not exist yet. Failures other than missing paths retain the original spelling.
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
