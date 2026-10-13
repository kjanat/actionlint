// Package projectpath provides hints for unsuccessful workflow discovery.
package projectpath

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// RepositoryRoot returns the nearest directory containing .git, or "" outside a repository.
func RepositoryRoot(start string) string {
	dir, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// HasWorkflowFiles reports whether any YAML file exists under the repository's workflows directory.
func HasWorkflowFiles(root string) (bool, error) {
	found := false
	err := filepath.WalkDir(filepath.Join(root, ".github", "workflows"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && (strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".yaml")) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return found, err
}

// WorkflowTypo returns a misspelled workflow directory and its expected path.
// It only examines the nearest repository, and never selects files for linting.
func WorkflowTypo(start string) (found, expected string) {
	dir := RepositoryRoot(start)
	if dir == "" {
		return "", ""
	}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if !entry.IsDir() || !near(entry.Name(), ".github") {
			continue
		}
		parent := filepath.Join(dir, entry.Name())
		children, _ := os.ReadDir(parent)
		for _, child := range children {
			if child.IsDir() && near(child.Name(), "workflows") && (entry.Name() != ".github" || child.Name() != "workflows") {
				return filepath.Join(parent, child.Name()), filepath.Join(dir, ".github", "workflows")
			}
		}
	}
	return "", ""
}

// Recognize one insertion, deletion, substitution, or adjacent transposition.
func near(name, want string) bool {
	name = strings.ToLower(name)
	if name == want {
		return true
	}
	a, b := []rune(name), []rune(want)
	if len(a) > len(b) {
		a, b = b, a
	}
	if len(b)-len(a) > 1 {
		return false
	}
	for i := range a {
		if a[i] == b[i] {
			continue
		}
		if len(a) != len(b) {
			return string(a[i:]) == string(b[i+1:])
		}
		return string(a[i+1:]) == string(b[i+1:]) ||
			(i+1 < len(a) && a[i] == b[i+1] && a[i+1] == b[i] && string(a[i+2:]) == string(b[i+2:]))
	}
	return true
}
