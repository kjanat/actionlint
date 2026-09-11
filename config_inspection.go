package actionlint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v4"
)

// ConfigSelection selects an explicit file, repository discovery, or no configuration.
type ConfigSelection struct {
	Path     string
	Disabled bool
}

// ConfigOrigin identifies whether a setting came from a default or a YAML value.
type ConfigOrigin struct {
	Source string `json:"source" yaml:"source"`
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
	cfg := &Config{}
	document := &yaml.Node{}
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return ConfigInspection{Path: path}, fmt.Errorf("could not read config file %q: %w", path, err)
		}
		cfg, document, err = parseConfigDocument(data)
		if err != nil {
			return ConfigInspection{Path: path}, fmt.Errorf("could not parse config file %q: %w", path, err)
		}
	}
	result := ConfigInspection{Path: path, Config: effectiveConfig(cfg)}
	if withOrigin {
		result.Origins = map[string]ConfigOrigin{}
		var defaults func(map[string]any, string)
		defaults = func(values map[string]any, prefix string) {
			for key, value := range values {
				pointer := prefix + "/" + configPointerPart(key)
				result.Origins[pointer] = ConfigOrigin{Source: "default", State: "missing"}
				if nested, ok := value.(map[string]any); ok {
					defaults(nested, pointer)
				}
			}
		}
		defaults(result.Config, "")
		if err := configOrigins(document, "", result.Origins); err != nil {
			return ConfigInspection{}, err
		}
	}
	return result, nil
}
