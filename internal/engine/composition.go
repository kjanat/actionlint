package engine

import (
	"fmt"

	"actionlint.kjanat.dev/internal/builtin"

	"actionlint.kjanat.dev/internal/tools"
)

// registeredFactory selects semantic rules and registered script tools. The
// document runner owns neither executable selection nor initialization policy.
func registeredFactory(request Request, logger analysisLogger) func(ruleContext, bool) (builtinAnalyzers, error) {
	return func(c ruleContext, action bool) (builtinAnalyzers, error) {
		var result builtinAnalyzers
		rulesContext := c.builtinContext()
		for _, descriptor := range builtin.Descriptors() {
			if descriptor.Build == nil && descriptor.Script == nil || action && !descriptor.SupportsActions {
				continue
			}
			if descriptor.Enabled != nil && !descriptor.Enabled(rulesContext) {
				continue
			}
			if descriptor.Build != nil {
				result.rules = append(result.rules, descriptor.Build(rulesContext))
			}
			if descriptor.Script != nil {
				result.scripts = append(result.scripts, factScriptAnalyzer{descriptor.Script})
				result.scriptInfo = append(result.scriptInfo, descriptor.Descriptor)
			}
		}

		context := tools.Context{Config: c.config, ProjectConfig: c.projectConfig, WorkingDir: c.workingDir, ProjectRoot: c.projectRoot, Processes: c.process, LoadEngine: request.LoadScriptEngine, Debug: logger.debug, Warn: func(err error) {
			if logger.logOut != nil {
				_, _ = fmt.Fprintln(logger.logOut, "warning:", err)
			}
		}}
		for _, registration := range request.Tools {
			if registration.Enabled != nil && !registration.Enabled(context) {
				continue
			}
			adapter, err := registration.Build(context)
			if err != nil {
				if registration.Required == nil || registration.Required(context) {
					return result, err
				}
				logger.log(fmt.Sprintf("Tool %q was disabled:", registration.Descriptor.Name), err)
				continue
			}
			result.scripts = append(result.scripts, registeredTool{adapter})
			result.scriptInfo = append(result.scriptInfo, registration.Descriptor)
			result.resolveExecution = result.resolveExecution || registration.RequiresExecution
		}
		return result, nil
	}
}
