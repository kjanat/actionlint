package engine

import (
	"fmt"

	"actionlint.kjanat.dev/internal/tools"
)

// registeredFactory selects semantic rules and registered script tools. The
// document runner owns neither executable selection nor initialization policy.
func registeredFactory(request Request, logger analysisLogger) func(ruleContext, bool) (builtinAnalyzers, error) {
	return func(c ruleContext, action bool) (builtinAnalyzers, error) {
		var result builtinAnalyzers
		for _, descriptor := range builtinRuleDescriptors() {
			if (descriptor.build == nil && descriptor.buildScripts == nil) || (action && !descriptor.supportsActions) {
				continue
			}
			if descriptor.skipWhenDisabled && diagnosticLevel(c.config, descriptor.Name) == "off" {
				continue
			}
			if descriptor.enabled != nil && !descriptor.enabled(c) {
				continue
			}
			var rule Rule
			var script scriptAnalyzer
			var err error
			if descriptor.buildScripts != nil {
				script, err = descriptor.buildScripts(c)
			} else {
				rule, err = descriptor.build(c)
			}
			if err != nil {
				if descriptor.required != nil && descriptor.required(c) {
					return result, err
				}
				logger.log(fmt.Sprintf("Rule %q was disabled:", descriptor.Name), err)
				continue
			}
			if rule != nil {
				result.rules = append(result.rules, rule)
			}
			if script != nil {
				result.scripts = append(result.scripts, script)
				result.resolveExecution = result.resolveExecution || descriptor.requiresScriptExecution
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
