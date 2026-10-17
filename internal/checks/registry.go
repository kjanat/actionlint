// Package checks constructs configured semantic passes from builtin registrations.
package checks

import (
	"io"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/builtin"
	repoconfig "actionlint.kjanat.dev/internal/config"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
	"actionlint.kjanat.dev/rule"
)

type Selection struct {
	Rules      []workflowvisit.Rule
	Scripts    []analyzer.Analyzer
	ScriptInfo []rule.Descriptor
}

func Build(context builtin.Context, action bool) Selection {
	var selected Selection
	for _, descriptor := range builtin.Descriptors() {
		if descriptor.Build == nil && descriptor.Script == nil || action && !descriptor.SupportsActions {
			continue
		}
		if descriptor.Enabled != nil && !descriptor.Enabled(context) {
			continue
		}
		if descriptor.Build != nil {
			selected.Rules = append(selected.Rules, descriptor.Build(context))
		}
		if descriptor.Script != nil {
			selected.Scripts = append(selected.Scripts, descriptor.Script)
			selected.ScriptInfo = append(selected.ScriptInfo, descriptor.Descriptor)
		}
	}
	return selected
}

// Prepare binds invocation configuration and debug output after rule interception.
func Prepare(rules []workflowvisit.Rule, config *repoconfig.Config, debug io.Writer) {
	for _, r := range rules {
		if configured, ok := r.(interface{ SetConfig(*repoconfig.Config) }); ok && config != nil {
			configured.SetConfig(config)
		}
		if logged, ok := r.(interface{ EnableDebug(io.Writer) }); ok && debug != nil {
			logged.EnableDebug(debug)
		}
	}
}

func Describe(rules []workflowvisit.Rule) []rule.Descriptor {
	descriptions := make([]rule.Descriptor, 0, len(rules))
	for _, r := range rules {
		info, ok := repoconfig.FindRule(r.Name())
		metadata := info.Descriptor
		if !ok {
			metadata.Name = r.Name()
		}
		if described, ok := r.(interface{ Description() string }); ok {
			metadata.Description = described.Description()
		}
		descriptions = append(descriptions, metadata)
	}
	return descriptions
}

func Templates() []rule.Descriptor {
	var rules []rule.Descriptor
	for _, descriptor := range builtin.Descriptors() {
		if descriptor.Provider == "actionlint" && descriptor.Build == nil && descriptor.Script == nil {
			rules = append(rules, descriptor.Clone())
		}
	}
	return rules
}
