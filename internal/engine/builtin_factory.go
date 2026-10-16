package engine

import (
	"fmt"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/ruff"
)

// builtinFactory selects visitor adapters and script analyzers. The
// document runner owns neither executable selection nor initialization policy.
func builtinFactory(request AnalysisRequest, logger analysisLogger) func(ruleContext, bool) (builtinAnalyzers, error) {
	compatibility := &ruff.Compatibility{}
	return func(c ruleContext, action bool) (builtinAnalyzers, error) {
		c.shellcheckRunner, c.debug = request.ShellcheckRunner, logger.debug
		c.shellcheck, c.shellcheckOptions, c.shellcheckSettings = request.ShellCheck, request.ShellcheckOptions, request.ShellcheckSettings
		c.ruff, c.ruffOptions, c.ruffCompatibility = request.Ruff, request.RuffOptions, compatibility
		c.ruffBackend = request.RuffBackend
		c.shellcheckBackend = request.ShellcheckBackend
		if request.LoadScriptEngine != nil {
			c.scriptEngine = func(path string) (analyzer.ScriptBackend, error) {
				return request.LoadScriptEngine(c.process.ctx, path)
			}
		}
		c.ruffWarning = func(err error) {
			if logger.logOut != nil {
				_, _ = fmt.Fprintln(logger.logOut, "warning: skipping automatically discovered Ruff:", err)
			}
		}
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
				result.scriptInfo = append(result.scriptInfo, RuleInfo{Name: descriptor.Name, Description: descriptor.Description})
			}
		}
		return result, nil
	}
}
