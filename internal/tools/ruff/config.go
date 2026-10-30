package ruff

import (
	"actionlint.kjanat.dev/v2/internal/config"
	toolconfig "actionlint.kjanat.dev/v2/internal/config/tools"
)

// Config is the Ruff tool configuration consumed by the checker.
type Config struct {
	Enabled *bool
	Backend *toolconfig.Backend
	// Native contains Ruff's own options, using its target-version and lint mapping.
	// The selected Ruff engine validates these options. Null values reset defaults.
	Native map[string]any
	// TargetVersion overrides the versioned Python shell target.
	TargetVersion string
	// Select chooses rule codes or prefixes. Nil uses Ruff's defaults.
	Select []string
	// Ignore excludes rule codes or prefixes from the selection.
	Ignore []string
}

// configFor reads the tools.ruff entry of cfg. Configuration decoding folds
// the flat target-version, select and ignore keys into target-version,
// lint.select and lint.ignore of the inline settings, so every option reaches
// Ruff natively. A native target-version already wins over the target
// inferred from the step's shell.
func configFor(cfg *config.Config) Config {
	if cfg == nil {
		return Config{}
	}
	tool := cfg.Tools["ruff"]
	return Config{Enabled: tool.Enabled, Backend: tool.Backend, Native: tool.Config.Inline()}
}
