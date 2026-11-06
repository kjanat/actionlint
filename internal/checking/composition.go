package checking

import (
	"fmt"

	config "actionlint.kjanat.dev/v2/internal/input/configuration"

	ruleexec "actionlint.kjanat.dev/v2/internal/checking/executor/registration"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	scriptexecution "actionlint.kjanat.dev/v2/internal/checking/scripts"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	"actionlint.kjanat.dev/v2/internal/checking/selection"
)

type documentChecks struct {
	selection.Selection
	scripts          scriptexecution.Service
	resolveExecution bool
	registeredTools  []tools.Registration
}

func (s *analysisSession) buildChecks(context ruleexec.Inputs, toolContext tools.Context, action bool) (documentChecks, error) {
	rules, err := selection.CreateRules(ConfigurationRules(), context, action)
	selected := documentChecks{Selection: rules}
	if err != nil {
		return selected, err
	}
	for _, a := range selected.Scripts {
		selected.scripts.Analyzers = append(selected.scripts.Analyzers, scriptexecution.FactAnalyzer{Analyzer: a})
	}
	for _, registration := range s.tools {
		if registration.Enabled != nil && !registration.Enabled(toolContext) {
			continue
		}
		adapter, err := registration.Build(toolContext)
		if err != nil {
			if registration.Required == nil || registration.Required(toolContext) {
				return selected, err
			}
			s.log(fmt.Sprintf("Tool %q was disabled:", registration.Descriptor.Name), err)
			if toolContext.Skipped != nil {
				toolContext.Skipped(registration.Descriptor.Name)
			}
			continue
		}
		selected.scripts.Analyzers = append(selected.scripts.Analyzers, scriptexecution.Tool{Runner: adapter})
		selected.registeredTools = append(selected.registeredTools, registration)
		selected.ScriptInfo = append(selected.ScriptInfo, registration.Descriptor)
		selected.resolveExecution = selected.resolveExecution || registration.RequiresExecution
	}
	if s.onRulesCreated != nil && !action {
		selected.Rules = s.onRulesCreated(selected.Rules)
	}
	selection.ConfigureRules(selected.Rules, context.Config, s.debugWriter())
	return selected, nil
}

func (d documentChecks) inputSelectors(c tools.Context, cfg *config.Config) ([]checkinputs.ScriptInput, error) {
	c.Config, c.ProjectConfig, c.ProjectRoot = cfg, nil, ""
	var inputs []checkinputs.ScriptInput
	for _, registration := range d.registeredTools {
		if ConfigurationRules().DiagnosticLevel(cfg, registration.Descriptor.Name) == "off" || registration.Enabled != nil && !registration.Enabled(c) {
			continue
		}
		inputs = append(inputs, registration.ScriptInputs...)
		if registration.InputSelectors != nil {
			additional, err := registration.InputSelectors(c)
			if err != nil {
				return nil, err
			}
			inputs = append(inputs, additional...)
		}
	}
	return inputs, nil
}
