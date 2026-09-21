package actionlint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ConfigSelection selects an explicit file, repository discovery, or no configuration.
type ConfigSelection struct {
	Path     string
	Disabled bool
}

// ConfigOrigin identifies a default, configuration file value, or Action input.
type ConfigOrigin struct {
	Source string `json:"source" yaml:"source"`
	Input  string `json:"input,omitempty" yaml:"input,omitempty"`
	State  string `json:"state" yaml:"state"`
	Line   int    `json:"line,omitempty" yaml:"line,omitempty"`
	Column int    `json:"column,omitempty" yaml:"column,omitempty"`
}

// ConfigInspection contains effective settings and optional source locations.
type ConfigInspection struct {
	Path    string                  `json:"path" yaml:"path"`
	Config  map[string]any          `json:"config" yaml:"config"`
	Origins map[string]ConfigOrigin `json:"origins,omitempty" yaml:"origins,omitempty"`
}

// SelectedConfigPath finds the selected config without parsing its contents.
func SelectedConfigPath(selection ConfigSelection) (string, error) {
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
	project, err := findProjectConfig(cwd, true)
	if err != nil || project == nil {
		return "", err
	}
	for _, name := range []string{"actionlint.yaml", "actionlint.yml"} {
		path := filepath.Join(project.RootDir(), ".github", name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return "", nil
}

// InspectConfig validates settings and optionally records their YAML origins.
func InspectConfig(selection ConfigSelection, withOrigin bool) (ConfigInspection, error) {
	path, err := SelectedConfigPath(selection)
	if err != nil {
		return ConfigInspection{}, err
	}
	var data []byte
	if path != "" {
		data, err = os.ReadFile(path)
		if err != nil {
			return ConfigInspection{Path: path}, fmt.Errorf("could not read config file %q: %w", path, err)
		}
	}
	resolved, err := resolveConfigDocument(data)
	if err != nil {
		return ConfigInspection{Path: path}, fmt.Errorf("could not parse config file %q: %w", path, err)
	}
	result := ConfigInspection{Path: path, Config: resolved.values}
	if withOrigin {
		result.Origins = resolved.origins
	}
	return result, nil
}
