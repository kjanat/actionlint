// Package selection constructs configured semantic passes from builtin registrations.
package selection

import (
	"fmt"
	"io"

	"actionlint.kjanat.dev/v2/internal/checking/define"

	"actionlint.kjanat.dev/v2/internal/checking/executor/registration"
	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/rulebinding"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/repository"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

type Selection struct {
	Unavailable []UnavailableCheck
	Rules       []workflowvisit.Rule
	Scripts     []checkinputs.Checker
	ScriptInfo  []rule.Descriptor
}

// UnavailableCheck identifies a selected check whose required inputs are unknown.
type UnavailableCheck struct {
	Name, Reason string
}

func CreateRules(ruleSet configuration.RuleSet, inputs define.RuleInputs, action bool) (Selection, error) {
	inputs.Rules = ruleSet
	var selected Selection
	for _, descriptor := range registration.Registrations(ruleSet) {
		if descriptor.Build == nil && descriptor.Script == nil || action && !descriptor.SupportsActions {
			continue
		}
		prepared := inputs
		prepared.Config = inputs.Bindings.Rule(descriptor.Name).Config()
		if descriptor.Enabled != nil && !descriptor.Enabled(prepared) {
			continue
		}
		if descriptor.Prepare != nil {
			outcome, err := descriptor.Prepare(prepared)
			if err != nil {
				return selected, fmt.Errorf("prepare %s inputs: %w", descriptor.Name, err)
			}
			switch outcome := outcome.(type) {
			case define.PreparedInputs:
				prepared = outcome.RuleInputs
			case define.UnavailableInputs:
				selected.Unavailable = append(selected.Unavailable, UnavailableCheck{Name: descriptor.Name, Reason: outcome.Reason})
				continue
			default:
				return selected, fmt.Errorf("prepare %s inputs: unsupported preparation result %T", descriptor.Name, outcome)
			}
		}
		if descriptor.Build != nil {
			r := descriptor.Build(prepared)
			if consumer, ok := r.(interface{ SetRepository(repository.Snapshot) }); ok {
				consumer.SetRepository(prepared.Repository)
			}
			selected.Rules = append(selected.Rules, r)
		}
		if descriptor.Script != nil {
			selected.Scripts = append(selected.Scripts, descriptor.Script)
			selected.ScriptInfo = append(selected.ScriptInfo, descriptor.Descriptor)
		}
	}
	return selected, nil
}

// ConfigureRules binds invocation configuration and debug output after rule interception.
func ConfigureRules(rules []workflowvisit.Rule, scope *rulebinding.Scope, debug io.Writer) {
	for _, r := range rules {
		config := scope.Rule(r.Name()).Config()
		if configured, ok := r.(interface{ SetBindings(*rulebinding.Scope) }); ok {
			configured.SetBindings(scope)
		} else if configured, ok := r.(interface{ SetConfig(*configuration.Config) }); ok && config != nil {
			configured.SetConfig(config)
		}
		if logged, ok := r.(interface{ EnableDebug(io.Writer) }); ok && debug != nil {
			logged.EnableDebug(debug)
		}
	}
}

func Describe(ruleSet configuration.RuleSet, rules []workflowvisit.Rule) []rule.Descriptor {
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

func Templates(ruleSet configuration.RuleSet) []rule.Descriptor {
	var rules []rule.Descriptor
	for _, descriptor := range registration.Registrations(ruleSet) {
		if descriptor.Provider == "actionlint" && descriptor.Build == nil && descriptor.Script == nil {
			rules = append(rules, descriptor.Descriptor.Clone())
		}
	}
	return rules
}
