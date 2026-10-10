package ruff

import (
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
)

// SupportedTargetVersions lists the Python targets accepted by Ruff.
func SupportedTargetVersions() []string {
	return []string{"py37", "py38", "py39", "py310", "py311", "py312", "py313", "py314", "py315"}
}

// Config controls isolated analysis of embedded Python scripts.
type Config struct {
	Enabled *bool `yaml:"enabled" jsonschema:"nullable,default=true"`
	// TargetVersion is Ruff's Python target; omission uses py314.
	TargetVersion string `yaml:"target-version,omitempty"`
	// Select chooses rule codes or prefixes. Omission selects F.
	Select []string `yaml:"select,omitempty" jsonschema:"nullable"`
	// Ignore excludes rule codes or prefixes from the selection.
	Ignore []string `yaml:"ignore,omitempty" jsonschema:"nullable"`
}

// MarshalYAML preserves the distinction between the default selection and an
// explicit empty selection during per-file composition and API round trips.
func (c Config) MarshalYAML() (any, error) {
	value := map[string]any{}
	if c.Enabled != nil {
		value["enabled"] = *c.Enabled
	}
	if c.TargetVersion != "" {
		value["target-version"] = c.TargetVersion
	}
	if c.Select != nil {
		value["select"] = c.Select
	}
	if c.Ignore != nil {
		value["ignore"] = c.Ignore
	}
	return value, nil
}

// UnmarshalYAML validates tool settings before an external process is started.
func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!bool" {
		var enabled bool
		if err := node.Decode(&enabled); err != nil {
			return err
		}
		*c = Config{Enabled: &enabled}
		return nil
	}
	type plain Config
	var next plain
	if err := node.Load(&next, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return err
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == "target-version" {
			var target *string
			if err := node.Content[i+1].Decode(&target); err != nil {
				return err
			}
			if target != nil && *target == "" {
				return fmt.Errorf("tools.ruff.target-version must not be empty at line %d", node.Content[i+1].Line)
			}
		}
		if node.Content[i].Value == "enabled" {
			value := node.Content[i+1]
			if value.Tag != "!!bool" && value.Tag != "!!null" {
				return fmt.Errorf("tools.ruff: enabled must be a boolean at line %d", value.Line)
			}
		}
	}
	if next.TargetVersion != "" && !slices.Contains(SupportedTargetVersions(), next.TargetVersion) {
		return fmt.Errorf("tools.ruff.target-version must be one of %s; got %q", strings.Join(SupportedTargetVersions(), ", "), next.TargetVersion)
	}
	for _, selectors := range [][]string{next.Select, next.Ignore} {
		for _, selector := range selectors {
			if _, ok := slices.BinarySearch(supportedRuleSelectors, selector); !ok {
				return fmt.Errorf("tools.ruff: invalid rule selector %q", selector)
			}
		}
	}
	*c = Config(next)
	return nil
}
