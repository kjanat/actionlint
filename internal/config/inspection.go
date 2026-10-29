package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/fsread"
)

// Selection selects an explicit file, repository discovery, or no configuration.
type Selection struct {
	Path     string
	Disabled bool
}

// SelectedPath finds the selected config without parsing its contents.
func SelectedPath(selection Selection) (string, error) {
	if selection.Disabled {
		return "", nil
	}
	if selection.Path != "" {
		return selection.Path, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root := repositoryRoot(cwd)
	if root == "" {
		return "", nil
	}
	for _, name := range RepositoryConfigNames {
		path := filepath.Join(root, ".github", name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return "", nil
}

// Inspect validates settings and optionally records their YAML origins.
func Inspect(selection Selection, withOrigin bool, registeredRules ...string) (Inspection, error) {
	path, err := SelectedPath(selection)
	if err != nil {
		return Inspection{}, err
	}
	var resolved Resolved
	if path != "" {
		var source *Loaded
		source, err = Read(path, fsread.Disk, registeredRules...)
		if err != nil {
			return Inspection{Path: path}, err
		}
		resolved = source.Resolved
	} else {
		resolved, err = ResolveDocument(nil, registeredRules...)
	}
	if err != nil {
		return Inspection{Path: path}, fmt.Errorf("could not parse config file %q: %w", path, err)
	}
	result := Inspection{Path: path, Config: resolved.Values, Warnings: resolved.Warnings}
	if withOrigin {
		result.Origins = resolved.Origins
	}
	return result, nil
}

func repositoryRoot(start string) string {
	dir := absPath(start)
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
