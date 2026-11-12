package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"

	"actionlint.kjanat.dev/v2/internal/input/selection"
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

// DecodeConfig accepts a list of mappings and rejects empty list entries.
func (c *IgnoreConfigs) DecodeConfig(value any) error {
	if value == nil {
		*c = nil
		return nil
	}
	list, ok := value.([]any)
	if !ok {
		return errors.New("ignores must be a list of selector mappings")
	}
	next := make(IgnoreConfigs, 0, len(list))
	for i, entry := range list {
		if _, ok := entry.(map[string]any); !ok {
			return binding.At(strconv.Itoa(i), errors.New("ignores entries must be selector mappings"))
		}
		var selected IgnoreConfig
		if err := selected.DecodeConfig(entry); err != nil {
			return binding.At(strconv.Itoa(i), err)
		}
		next = append(next, selected)
	}
	*c = next
	return nil
}

// DecodeConfig rejects ineffective selectors and unknown fields.
func (c *IgnoreConfig) DecodeConfig(value any) error {
	object, ok := value.(map[string]any)
	if !ok {
		return errors.New("ignore must be a selector mapping")
	}
	type plain IgnoreConfig
	var next plain
	if err := binding.Decode(object, &next); err != nil {
		return err
	}
	for key, value := range object {
		selected, ok := value.(string)
		if !ok || strings.TrimSpace(selected) == "" {
			return binding.At(key, errors.New("ignore selector must be a nonempty string"))
		}
	}
	if err := IgnoreConfig(next).validateStructure(); err != nil {
		return err
	}
	*c = IgnoreConfig(next)
	return nil
}

func (ruleSet RuleSet) ValidateIgnore(c IgnoreConfig, additional ...string) error {
	if ruleSet.ResolveAlias(c.Rule) == "forbidden-suppression" {
		return errors.New("ignores: forbidden-suppression cannot be suppressed")
	}
	if !ruleSet.Suppressible(c.Rule, additional...) {
		return fmt.Errorf("ignores: unknown suppressible rule %q", c.Rule)
	}
	return c.validateStructure()
}

func (c IgnoreConfig) validateStructure() error {
	if c.Rule == "forbidden-suppression" {
		return errors.New("ignores: forbidden-suppression cannot be suppressed")
	}
	if strings.TrimSpace(c.Rule) == "" {
		return errors.New("ignores: rule must not be empty")
	}
	if c.File == "" && c.Uses == "" && c.Job == "" && c.Step == "" {
		return fmt.Errorf("ignores: rule %q requires at least one file, uses, job, or step selector", c.Rule)
	}
	if c.File != "" && (!selection.ValidPattern(c.File) || strings.HasPrefix(c.File, "!")) {
		return fmt.Errorf("ignores: invalid repository-relative file glob %q", c.File)
	}
	return nil
}
