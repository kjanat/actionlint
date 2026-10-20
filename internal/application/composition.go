package application

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/builtin"
	"actionlint.kjanat.dev/v2/internal/checks"
	"actionlint.kjanat.dev/v2/internal/tools"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"
)

type documentChecks struct {
	checks.Selection
	scripts          scriptexecution.Service
	resolveExecution bool
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
		selected.ScriptInfo = append(selected.ScriptInfo, registration.Descriptor)
		selected.resolveExecution = selected.resolveExecution || registration.RequiresExecution
	}
	if s.onRulesCreated != nil && !action {
		selected.Rules = s.onRulesCreated(selected.Rules)
	}
	checks.Prepare(selected.Rules, context.Config, s.debugWriter())
	return selected, nil
}
