package config

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/configuration/binding"
	lintconfig "actionlint.kjanat.dev/v2/internal/input/configuration/rules"
)

// DependencyCategories lists rule categories selected for dependency checks.
type DependencyCategories []string

// DependencyRules lists rule names or rule/code pairs selected for dependency checks.
type DependencyRules []string

// DependencyFindings controls whether advice from remote actions fails a check.
// Known execution errors always count. These settings do not enable disabled rules.
type DependencyFindings struct {
	// FailOnAll makes every enabled dependency finding count toward exit status 1.
	FailOnAll *bool `yaml:"fail-on-all,omitempty" jsonschema:"nullable,default=false"`
	// FailOnCategories selects rule categories whose dependency findings fail the check.
	FailOnCategories DependencyCategories `yaml:"fail-on-categories,omitempty" jsonschema:"nullable"`
	// FailOnRules accepts rule names and rule/code pairs, such as shellcheck/SC2086.
	FailOnRules DependencyRules `yaml:"fail-on-rules,omitempty" jsonschema:"nullable"`
}

func (c *DependencyFindings) DecodeConfig(value any) error {
	type plain DependencyFindings
	var next plain
	if err := binding.Decode(value, &next); err != nil {
		return err
	}
	for i, category := range next.FailOnCategories {
		if !slices.Contains(lintconfig.Categories(), category) {
			return binding.At("fail-on-categories", binding.At(strconv.Itoa(i), fmt.Errorf("unknown rule category %q", category)))
		}
	}
	*c = DependencyFindings(next)
	return nil
}

func (c DependencyFindings) EncodeConfig() (any, error) {
	value := map[string]any{}
	if c.FailOnAll != nil {
		value["fail-on-all"] = c.FailOnAll
	}
	if c.FailOnCategories != nil {
		value["fail-on-categories"] = c.FailOnCategories
	}
	if c.FailOnRules != nil {
		value["fail-on-rules"] = c.FailOnRules
	}
	return value, nil
}

func (ruleSet RuleSet) DependencyFindingFails(config *Config, name, code string, executionFailure bool) bool {
	if executionFailure {
		return true
	}
	if config == nil {
		return false
	}
	selection := config.Lint.Dependencies
	if selection.FailOnAll != nil && *selection.FailOnAll {
		return true
	}
	descriptor, _ := ruleSet.FindRule(name)
	if slices.Contains(selection.FailOnCategories, descriptor.Category) {
		return true
	}
	for _, selector := range selection.FailOnRules {
		selected, selectedCode, qualified := strings.Cut(selector, "/")
		if ruleSet.ResolveAlias(selected) == ruleSet.ResolveAlias(name) && (!qualified || code == selectedCode) {
			return true
		}
	}
	return false
}
