package application

import (
	"fmt"

	"actionlint.kjanat.dev/internal/config"

	"actionlint.kjanat.dev/analyzer"

	"actionlint.kjanat.dev/internal/builtin"
	"actionlint.kjanat.dev/internal/checks"
	"actionlint.kjanat.dev/internal/tools"
	scriptexecution "actionlint.kjanat.dev/internal/tools/execution"
)

type documentChecks struct {
	checks.Selection
	scripts          scriptexecution.Service
	resolveExecution bool
	registeredTools  []tools.Registration
}

func (s *analysisSession) buildChecks(context builtin.Context, toolContext tools.Context, action bool) (documentChecks, error) {
	selected := documentChecks{Selection: checks.Build(context, action)}
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
	checks.Prepare(selected.Rules, context.Config, s.debugWriter())
	return selected, nil
}

func (d documentChecks) inputSelectors(c tools.Context, cfg *config.Config) ([]analyzer.ScriptInput, error) {
	c.Config, c.ProjectConfig, c.ProjectRoot = cfg, nil, ""
	if cfg != nil {
		resolved := *cfg
		resolved.Overrides = nil
		c.Config = &resolved
	}
	var inputs []analyzer.ScriptInput
	for _, registration := range d.registeredTools {
		if config.DiagnosticLevel(cfg, registration.Descriptor.Name) == "off" || registration.Enabled != nil && !registration.Enabled(c) {
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
