package ruff

import (
	"errors"
	"fmt"
	"regexp"

	"go.yaml.in/yaml/v4"
)

var ruffTargetVersion = regexp.MustCompile(`^py3[0-9]+$`)
var ruffRuleSelector = regexp.MustCompile(`^[A-Z]+[0-9]*$`)

// Config controls isolated analysis of embedded Python scripts.
type Config struct {
	Enabled *bool `yaml:"enabled" jsonschema:"nullable,default=true"`
	// TargetVersion is Ruff's Python target; omission uses py314.
	TargetVersion string `yaml:"target-version,omitempty" jsonschema:"pattern=^py3[0-9]+$"`
	// Select chooses rule codes or prefixes. Omission selects F.
	Select []string `yaml:"select" jsonschema:"nullable"`
	// Ignore excludes rule codes or prefixes from the selection.
	Ignore []string `yaml:"ignore" jsonschema:"nullable"`
}

// UnmarshalYAML validates tool settings before an external process is started.
func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	type plain Config
	var next plain
	if err := node.Load(&next, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return err
	}
	if next.TargetVersion != "" && !ruffTargetVersion.MatchString(next.TargetVersion) {
		return errors.New("tools.ruff.target-version must be a Ruff Python target such as py314")
	}
	for _, selectors := range [][]string{next.Select, next.Ignore} {
		for _, selector := range selectors {
			if !ruffRuleSelector.MatchString(selector) {
				return fmt.Errorf("tools.ruff: invalid rule selector %q", selector)
			}
		}
	}
	*c = Config(next)
	return nil
}
