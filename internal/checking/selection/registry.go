// Package selection constructs configured semantic passes from builtin registrations.
package selection

import (
	"fmt"
	"io"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/results"

	"actionlint.kjanat.dev/v2/internal/checking/executor/registration"
	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/rulebinding"
	"actionlint.kjanat.dev/v2/internal/model/repository"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

// CheckSelection holds the workflow rules and script analyzers that
// configuration enables, and the checks that cannot run because their
// required inputs are unknown.
type CheckSelection struct {
	Unavailable []UnavailableCheck
	Rules       []workflowvisit.Rule
	Scripts     []results.Analyzer
	ScriptInfo  []rule.Descriptor
}

// UnavailableCheck identifies a selected check whose required inputs are unknown.
type UnavailableCheck struct {
	Name, Reason string
}

// CreateRules prepares and constructs the native rules that configuration
// enables, in presentation order. For an action document it selects only the
// rules that support actions. Every native rule that executes must be declared
// in ruleSet.
func CreateRules(ruleSet configuration.RuleSet, inputs define.RuleInputs, action bool) (CheckSelection, error) {
	inputs.Rules = ruleSet
	var selected CheckSelection
	for _, native := range registration.All {
		if !native.Executable() || action && !native.SupportsActions {
			continue
		}
		if _, declared := ruleSet.FindRule(native.Name); !declared {
			return selected, fmt.Errorf("rule %s is not declared", native.Name)
		}
		prepared := inputs
		prepared.Config = inputs.Bindings.Rule(native.Name).Config()
		if native.Enabled != nil && !native.Enabled(prepared) {
			continue
		}
		if native.Prepare != nil {
			outcome, err := native.Prepare(prepared)
			if err != nil {
				return selected, fmt.Errorf("prepare %s inputs: %w", native.Name, err)
			}
			switch outcome := outcome.(type) {
			case define.PreparedInputs:
				prepared = outcome.RuleInputs
			case define.UnavailableInputs:
				selected.Unavailable = append(selected.Unavailable, UnavailableCheck{Name: native.Name, Reason: outcome.Reason})
				continue
			default:
				return selected, fmt.Errorf("prepare %s inputs: unsupported preparation result %T", native.Name, outcome)
			}
		}
		if native.Visits() {
			r := native.NewVisitor(prepared)
			if consumer, ok := r.(interface{ SetRepository(repository.Settings) }); ok {
				consumer.SetRepository(prepared.Repository)
			}
			selected.Rules = append(selected.Rules, r)
		}
		if analyzer, ok := native.ScriptAnalyzer(); ok {
			selected.Scripts = append(selected.Scripts, analyzer)
			selected.ScriptInfo = append(selected.ScriptInfo, native.ResolvedDeclaration().Descriptor)
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

// Templates describes the declaration-only native rules, whose findings
// other components report.
func Templates() []rule.Descriptor {
	var rules []rule.Descriptor
	for _, native := range registration.All {
		if declaration := native.ResolvedDeclaration(); !native.Executable() && declaration.Provider == "actionlint" {
			rules = append(rules, declaration.Descriptor)
		}
	}
	return rules
}
