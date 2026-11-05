// Package selection constructs configured semantic passes from builtin registrations.
package selection

import (
	"io"

	ruleexec "actionlint.kjanat.dev/v2/internal/checking/executor/registration"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	repository "actionlint.kjanat.dev/v2/internal/model/repositories"
)

type Selection struct {
	Rules      []workflowvisit.Rule
	Scripts    []checkinputs.Checker
	ScriptInfo []rule.Descriptor
}

func CreateRules(ruleSet repoconfig.RuleSet, inputs ruleexec.Inputs, action bool) Selection {
	inputs.Rules = ruleSet
	var selected Selection
	for _, descriptor := range ruleexec.Registrations(ruleSet) {
		if descriptor.Build == nil && descriptor.Script == nil || action && !descriptor.SupportsActions {
			continue
		}
		if descriptor.Enabled != nil && !descriptor.Enabled(inputs) {
			continue
		}
		if descriptor.Build != nil {
			r := descriptor.Build(inputs)
			if consumer, ok := r.(interface{ SetRepository(repository.Snapshot) }); ok {
				consumer.SetRepository(inputs.Repository)
			}
			selected.Rules = append(selected.Rules, r)
		}
		if descriptor.Script != nil {
			selected.Scripts = append(selected.Scripts, descriptor.Script)
			selected.ScriptInfo = append(selected.ScriptInfo, descriptor.Descriptor)
		}
	}
	return selected
}

// ConfigureRules binds invocation configuration and debug output after rule interception.
func ConfigureRules(rules []workflowvisit.Rule, config *repoconfig.Config, debug io.Writer) {
	for _, r := range rules {
		if configured, ok := r.(interface{ SetConfig(*repoconfig.Config) }); ok && config != nil {
			configured.SetConfig(config)
		}
		if logged, ok := r.(interface{ EnableDebug(io.Writer) }); ok && debug != nil {
			logged.EnableDebug(debug)
		}
	}
}

func Describe(ruleSet repoconfig.RuleSet, rules []workflowvisit.Rule) []rule.Descriptor {
	descriptions := make([]rule.Descriptor, 0, len(rules))
	for _, r := range rules {
		info, ok := ruleSet.FindRule(r.Name())
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

func Templates(ruleSet repoconfig.RuleSet) []rule.Descriptor {
	var rules []rule.Descriptor
	for _, descriptor := range ruleexec.Registrations(ruleSet) {
		if descriptor.Provider == "actionlint" && descriptor.Build == nil && descriptor.Script == nil {
			rules = append(rules, descriptor.Clone())
		}
	}
	return rules
}
