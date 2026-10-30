package shellcheck

import (
	"actionlint.kjanat.dev/v2/internal/config"
	toolconfig "actionlint.kjanat.dev/v2/internal/config/tools"
)

// inlineConfig decodes the inline tools.shellcheck.config directives of cfg.
// It returns nil when cfg selects none, including when it selects an rc path.
func inlineConfig(cfg *config.Config) (*ShellcheckConfig, error) {
	if cfg == nil {
		return nil, nil
	}
	inline := cfg.Tools["shellcheck"].Config.Inline()
	if inline == nil {
		return nil, nil
	}
	return toolconfig.ParseShellcheckConfig(inline)
}
