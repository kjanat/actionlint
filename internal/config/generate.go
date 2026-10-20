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

	"actionlint.kjanat.dev/internal/buildinfo"
	"actionlint.kjanat.dev/internal/fsread"
)

// Generate creates the repository configuration without producing terminal output.
// skipConfig avoids parsing an existing configuration before checking for a collision.
func Generate(root string, skipConfig bool, version string) (string, error) {
	if root == "" {
		return "", errors.New("project is not found. check current project is initialized as Git repository and \".github/workflows\" directory exists")
	}
	if !skipConfig {
		if _, _, err := LoadRepository(root, fsread.New(context.Background(), nil, nil).ReadFile); err != nil {
			return "", err
		}
	}
	directory := filepath.Join(root, ".github")
	for _, name := range []string{"actionlint.yaml", "actionlint.yml"} {
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
	info, _ := debug.ReadBuildInfo()
	ref := buildinfo.SourceRef(version, info)
	b := []byte(strings.Replace(defaultConfigTemplate, "/HEAD/actionlint.schema.json", "/"+ref+"/actionlint.schema.json", 1))
	if err := os.WriteFile(path, b, 0644); err != nil {
		return fmt.Errorf("could not write default configuration file at %q: %w", path, err)
	}
	return nil
}
