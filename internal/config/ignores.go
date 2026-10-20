package config

import (
	"errors"
	"fmt"
	"strings"

	"actionlint.kjanat.dev/internal/filefilter"

	"go.yaml.in/yaml/v4"
)

// IgnoreConfig suppresses one rule where all supplied selectors match.
type IgnoreConfig struct {
	// Rule is a registered diagnostic rule ID.
	Rule string `yaml:"rule" jsonschema:"required"`
	// File is a repository-relative path or doublestar glob.
	File string `yaml:"file,omitempty"`
	// Uses matches an action or reusable workflow reference exactly, including its version.
	Uses string `yaml:"uses,omitempty"`
	// Job matches a workflow job ID exactly.
	Job string `yaml:"job,omitempty"`
	// Step matches a step ID or name exactly.
	Step string `yaml:"step,omitempty"`
	// Reason optionally explains why this exception is needed.
	Reason string `yaml:"reason,omitempty"`
}

// IgnoreConfigs is the reusable suppression list.
type IgnoreConfigs []IgnoreConfig

// UnmarshalYAML accepts a list of mappings and rejects empty list entries.
func (c *IgnoreConfigs) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.SequenceNode {
		return errors.New("ignores must be a list of selector mappings")
	}
	next := make(IgnoreConfigs, 0, len(n.Content))
	for _, entry := range n.Content {
		if entry.Kind != yaml.MappingNode {
			return errors.New("ignores entries must be selector mappings")
		}
		var ignore IgnoreConfig
		if err := entry.Decode(&ignore); err != nil {
			return err
		}
		next = append(next, ignore)
	}
	*c = next
	return nil
}

// UnmarshalYAML rejects ineffective selectors and unknown fields.
func (c *IgnoreConfig) UnmarshalYAML(n *yaml.Node) error {
	type plain IgnoreConfig
	var next plain
	if err := n.Load(&next, yaml.WithV3Defaults(), yaml.WithKnownFields()); err != nil {
		return err
	}
	for i := 0; i < len(n.Content); i += 2 {
		value := n.Content[i+1]
		if value.Kind != yaml.ScalarNode || value.ShortTag() != "!!str" || strings.TrimSpace(value.Value) == "" {
			return fmt.Errorf("ignores.%s must be a nonempty string", n.Content[i].Value)
		}
	}
	if err := IgnoreConfig(next).validateStructure(); err != nil {
		return err
	}
	*c = IgnoreConfig(next)
	return nil
}

func (c IgnoreConfig) Validate(additional ...string) error {
	if !Suppressible(c.Rule, additional...) {
		return fmt.Errorf("ignores: unknown suppressible rule %q", c.Rule)
	}
	return c.validateStructure()
}

func (c IgnoreConfig) validateStructure() error {
	if c.Rule == "disallow-suppressions" {
		return errors.New("ignores: disallow-suppressions cannot be suppressed")
	}
	if strings.TrimSpace(c.Rule) == "" {
		return errors.New("ignores: rule must not be empty")
	}
	if c.File == "" && c.Uses == "" && c.Job == "" && c.Step == "" {
		return fmt.Errorf("ignores: rule %q requires at least one file, uses, job, or step selector", c.Rule)
	}
	if c.File != "" && (!filefilter.ValidPattern(c.File) || strings.HasPrefix(c.File, "!")) {
		return fmt.Errorf("ignores: invalid repository-relative file glob %q", c.File)
	}
	return nil
}
