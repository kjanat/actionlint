package checking

import (
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking/define"

	"actionlint.kjanat.dev/v2/internal/input/configuration/rulebinding"

	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/scripts"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	"actionlint.kjanat.dev/v2/internal/checking/selection"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

type documentChecks struct {
	selection.Selection
	scripts          scripts.Service
	resolveExecution bool
	registeredTools  []contract.Registration
}

func (s *analysisSession) buildChecks(context define.RuleInputs, toolContext contract.Context, action bool) (documentChecks, error) {
	rules, err := selection.CreateRules(ConfigurationRules(), context, action)
	selected := documentChecks{Selection: rules}
	if err != nil {
		return selected, err
	}
	for _, a := range selected.Scripts {
		execution := slices.Contains(a.Descriptor().RequiredInputs, rule.Execution)
		selected.scripts.Analyzers = append(selected.scripts.Analyzers, scripts.FactAnalyzer{Analyzer: a, IncludeExecution: execution})
		selected.resolveExecution = selected.resolveExecution || execution
	}
	for _, registration := range s.tools {
		toolContext.Config = context.Bindings.Rule(registration.Descriptor.Name).Config()
		owned, err := toolContext.Detached()
		if err != nil {
			return selected, err
		}
		if registration.Enabled != nil && !registration.Enabled(owned) {
			continue
		}
		adapter, err := registration.Build(owned)
		if err != nil {
			if registration.Required == nil || registration.Required(owned) {
				return selected, err
			}
			s.log(fmt.Sprintf("Tool %q was disabled:", registration.Name), err)
			if toolContext.Skipped != nil {
				toolContext.Skipped(registration.Name)
			}
			continue
		}
		selected.scripts.Analyzers = append(selected.scripts.Analyzers, contract.Tool{Name: registration.Name, Runner: adapter})
		selected.registeredTools = append(selected.registeredTools, registration)
		selected.ScriptInfo = append(selected.ScriptInfo, registration.Descriptor)
		selected.resolveExecution = selected.resolveExecution || registration.RequiresExecution
	}
	if s.onRulesCreated != nil && !action {
		selected.Rules = s.onRulesCreated(selected.Rules)
	}
	selection.ConfigureRules(selected.Rules, context.Bindings, s.debugWriter())
	return selected, nil
}

func (d documentChecks) inputSelectors(c contract.Context, scope *rulebinding.Scope) ([]inputs.ScriptInput, error) {
	c.ProjectConfig, c.ProjectRoot = nil, ""
	var inputs []inputs.ScriptInput
	for _, registration := range d.registeredTools {
		cfg := scope.Rule(registration.Descriptor.Name).Config()
		c.Config = cfg
		owned, err := c.Detached()
		if err != nil {
			return nil, err
		}
		if ConfigurationRules().DiagnosticLevel(cfg, registration.Name) == "off" || registration.Enabled != nil && !registration.Enabled(owned) {
			continue
		}
		inputs = append(inputs, registration.ScriptInputs...)
		if registration.InputSelectors != nil {
			additional, err := registration.InputSelectors(owned)
			if err != nil {
				return nil, err
			}
			inputs = append(inputs, additional...)
		}
	}
	return inputs, nil
}
