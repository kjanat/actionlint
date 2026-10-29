package config

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"actionlint.kjanat.dev/v2/internal/buildinfo"
	"actionlint.kjanat.dev/v2/internal/fsread"
)

// Generate creates the repository configuration without producing terminal output.
// skipConfig avoids parsing an existing configuration before checking for a collision.
// Reading the existing configuration stops when ctx is canceled.
func Generate(ctx context.Context, root string, skipConfig bool, version string) (string, error) {
	if root == "" {
		return "", errors.New("project is not found. check current project is initialized as Git repository and \".github/workflows\" directory exists")
	}
	if !skipConfig {
		if _, _, err := LoadRepository(root, fsread.New(ctx, nil, nil).ReadFile); err != nil {
			return "", err
		}
	}
	directory := filepath.Join(root, ".github")
	for _, name := range RepositoryConfigNames {
		path := filepath.Join(directory, name)
		if _, err := os.Stat(path); err == nil {
			return "", fmt.Errorf("config file already exists at %q", path)
		}
	}
	path := filepath.Join(directory, "actionlint.yaml")
	if err := os.MkdirAll(directory, 0755); err != nil {
		return "", fmt.Errorf("could not create config directory %q: %w", directory, err)
	}
	if err := WriteDefaultFile(path, version); err != nil {
		return "", err
	}
	return path, nil
}

//go:embed config_default.yaml
var defaultConfigTemplate string

func WriteDefaultFile(path, version string) error {
	b := DefaultDocument(version)
	if err := os.WriteFile(path, b, 0644); err != nil {
		return fmt.Errorf("could not write default configuration file at %q: %w", path, err)
	}
	return nil
}

// DefaultDocument returns the current configuration template without writing it.
func DefaultDocument(version string) []byte {
	info, _ := debug.ReadBuildInfo()
	ref := buildinfo.SourceRef(version, info)
	return []byte(strings.Replace(defaultConfigTemplate, "/HEAD/actionlint.schema.json", "/"+ref+"/actionlint.schema.json", 1))
}
