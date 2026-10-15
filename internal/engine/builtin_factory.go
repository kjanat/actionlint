package engine

import (
	"actionlint.kjanat.dev/internal/ruff"
	"fmt"
)

// builtinFactory is the composition root for legacy traversal adapters. The
// document runner owns neither executable selection nor initialization policy.
func builtinFactory(request AnalysisRequest, logger analysisLogger) func(ruleContext, bool) ([]Rule, error) {
	compatibility := &ruff.Compatibility{}
	return func(c ruleContext, action bool) ([]Rule, error) {
		c.shellcheck, c.shellcheckOptions, c.shellcheckSettings = request.ShellCheck, request.ShellcheckOptions, request.ShellcheckSettings
		c.ruff, c.ruffOptions, c.ruffCompatibility = request.Ruff, request.RuffOptions, compatibility
		c.ruffWarning = func(err error) {
			if logger.logOut != nil {
				_, _ = fmt.Fprintln(logger.logOut, "warning: skipping automatically discovered Ruff:", err)
			}
		}
		var rules []Rule
		for _, descriptor := range builtinRuleDescriptors() {
			if descriptor.build == nil || (action && !descriptor.supportsActions) {
				continue
			}
			if descriptor.skipWhenDisabled && c.config.diagnosticLevel(descriptor.Name) == "off" {
				continue
			}
			if descriptor.enabled != nil && !descriptor.enabled(c) {
				continue
			}
			rule, err := descriptor.build(c)
			if err != nil {
				if descriptor.required != nil && descriptor.required(c) {
					return nil, err
				}
				logger.log(fmt.Sprintf("Rule %q was disabled:", descriptor.Name), err)
				continue
			}
			rules = append(rules, rule)
		}
		return rules, nil
	}
}
