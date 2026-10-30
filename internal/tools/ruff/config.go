package ruff

import (
	"fmt"

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
// the flat target-version, select and ignore keys into the inline settings.
// A target-version stays a native option, which already wins over the target
// inferred from the step's shell. Select and ignore are split back out, so an
// explicit empty selection stays distinct from the default one.
func configFor(cfg *config.Config) (Config, error) {
	if cfg == nil {
		return Config{}, nil
	}
	tool := cfg.Tools["ruff"]
	settings := Config{Enabled: tool.Enabled, Backend: tool.Backend, Native: tool.Config.Inline()}
	for key, selection := range map[string]*[]string{"select": &settings.Select, "ignore": &settings.Ignore} {
		value, exists := settings.Native[key]
		if !exists {
			continue
		}
		delete(settings.Native, key)
		if value == nil {
			continue
		}
		entries, ok := value.([]any)
		if !ok {
			return Config{}, fmt.Errorf("tools.ruff.config.%s must be a list of rule selectors", key)
		}
		*selection = make([]string, len(entries))
		for i, entry := range entries {
			selector, ok := entry.(string)
			if !ok || selector == "" {
				return Config{}, fmt.Errorf("tools.ruff.config.%s: rule selector must be a nonempty string", key)
			}
			(*selection)[i] = selector
		}
	}
	return settings, nil
}
