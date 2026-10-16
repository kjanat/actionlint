package repoconfig

import (
	"actionlint.kjanat.dev/internal/lintconfig"
)

// Level controls the severity of a diagnostic.
type Level = lintconfig.Level

// Preset selects a rule baseline.
type Preset = lintconfig.Preset

// Setting provides a level and typed per-rule options.
type Setting = lintconfig.Setting

// Group selects rules within a concern.
type Group = lintconfig.Group

func (c *LintRulesConfig) Groups() map[string]Group {
	return map[string]Group{"correctness": c.Correctness, "suspicious": c.Suspicious, "security": c.Security, "policy": c.Policy, "external": c.External, "nursery": c.Nursery, "plugins": c.Plugins}
}
