package actionlint

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/internal/filefilter"
	"actionlint.kjanat.dev/internal/suppression"
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
	if err := IgnoreConfig(next).validate(); err != nil {
		return err
	}
	*c = IgnoreConfig(next)
	return nil
}

func (c IgnoreConfig) validate() error {
	if !isInlineSuppressibleRule(c.Rule) {
		return fmt.Errorf("ignores: unknown suppressible rule %q", c.Rule)
	}
	if c.File == "" && c.Uses == "" && c.Job == "" && c.Step == "" {
		return fmt.Errorf("ignores: rule %q requires at least one file, uses, job, or step selector", c.Rule)
	}
	if c.File != "" && (!filefilter.ValidPattern(c.File) || strings.HasPrefix(c.File, "!")) {
		return fmt.Errorf("ignores: invalid repository-relative file glob %q", c.File)
	}
	return nil
}

func filterConfigIgnores(path, root string, source []byte, findings []*Error, cfg *Config, foreign bool, level func(string) RuleLevel) []*Error {
	if cfg == nil || len(cfg.Ignores) == 0 || len(findings) == 0 {
		return findings
	}
	file := filepath.ToSlash(repositoryRelativeConfigPath(path, root))
	contexts := suppression.Contexts(source, literalExpressionValue)
	filtered := make([]*Error, 0, len(findings))
	for _, finding := range findings {
		if level(finding.Kind) == "off" {
			filtered = append(filtered, finding)
			continue
		}
		line, column, applies := finding.Line, finding.Column, finding.source == nil || foreign
		if origin := finding.suppressionOrigin; origin != nil {
			line, applies = origin.first, origin.path == path || !foreign && origin.path == ""
			column = 0
		}
		ignored := false
		if applies && finding.Kind != "disallow-suppressions" {
			for _, ignore := range cfg.Ignores {
				if finding.Kind != ignore.Rule || ignore.validate() != nil {
					continue
				}
				if ignore.File != "" && !(filefilter.Selection{Includes: []string{ignore.File}}).Match(file) {
					continue
				}
				if !contexts.Match(line, column, ignore.Job, ignore.Step, ignore.Uses) {
					continue
				}
				mode := cfg.Policy.DisallowSuppressions.reportFor(ignore.Rule)
				if mode == reportSuppression || mode == reportAll {
					policyError := *finding
					policyError.Kind = "disallow-suppressions"
					policyError.Message = fmt.Sprintf("configured suppression of %s is disallowed by policy.disallow-suppressions", ignore.Rule)
					policyError.code, policyError.severity, policyError.fixes = "", "", nil
					filtered = append(filtered, &policyError)
				}
				ignored = mode == suppressionsAllowed || mode == reportSuppression
				break
			}
		}
		if !ignored {
			filtered = append(filtered, finding)
		}
	}
	return filtered
}
