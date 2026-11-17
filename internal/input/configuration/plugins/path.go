package plugins

import (
	"errors"
	"os"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
)

// FindOrDefaultPath selects an existing repository configuration in directory, or its default filename.
func FindOrDefaultPath(directory string) (string, error) {
	for _, name := range configuration.RepositoryConfigNames {
		path := filepath.Join(directory, name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return filepath.Join(directory, configuration.FormatYAML.Filename()), nil
}
