package config

import (
	rulescope "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// Rule projects a declaration into configuration selection and legacy policy fields.
// Execution stays with the checking implementation.
type Rule struct {
	rulescope.Descriptor
	Group           string
	SupportsActions bool
	Scope           rulescope.ConfigurationScope
	// DefaultLevel is the recommended severity; omission means on.
	DefaultLevel Level
	Options      func() any
	Configure    func(*Config, Setting)
}

func boolRuleOption(field func(*Policy) **bool) func(*Config, Setting) {
	return func(c *Config, s Setting) { *field(&c.Policy) = new(s.Level() != "off") }
}

func configureJobTimeout(c *Config, s Setting) {
	if s.Level() == "off" {
		c.Policy.RequireJobTimeout = nil
		return
	}
	if options := new(JobTimeoutPolicy); loadOptions(s, options) {
		c.Policy.RequireJobTimeout = options
	} else if !c.Policy.RequireJobTimeout.Enabled() {
		c.Policy.RequireJobTimeout = RequireJobTimeout(0)
	}
}

func configureSuppressionsPolicy(c *Config, s Setting) {
	if s.Level() == "off" {
		c.Policy.DisallowSuppressions = nil
		return
	}
	if options := new(SuppressionsPolicy); loadOptions(s, options) {
		options.names = c.Lint.Rules.ruleSet.Names()
		c.Policy.DisallowSuppressions = options
	} else if !c.Policy.DisallowSuppressions.Enabled() {
		c.Policy.DisallowSuppressions = &SuppressionsPolicy{report: ReportAll, names: c.Lint.Rules.ruleSet.Names()}
	}
}
