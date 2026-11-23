package backends

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/integrations/hostprocess"
)

// ProjectMayEnable preserves a tool when a calling workflow or composite can enable it.
func ProjectMayEnable(name string, config, projectConfig *configuration.Config) (bool, error) {
	return projectMayMatch(config, projectConfig, func(config *configuration.Config) (bool, error) { return mayEnable(name, config) })
}

func ProjectMayNeedProcess(name string, config, projectConfig *configuration.Config, command string, explicit *hostprocess.CommandOptions) (bool, error) {
	return ProjectMayNeedProcessFor(name, config, projectConfig, func(candidate *configuration.Config) bool {
		return hostprocess.CommandEnabled(command, explicit) && Module(name, command, explicit, candidate) == nil
	})
}

func ProjectMayNeedProcessFor(name string, config, projectConfig *configuration.Config, needsProcess func(*configuration.Config) bool) (bool, error) {
	return projectMayMatch(config, projectConfig, func(config *configuration.Config) (bool, error) {
		candidates, err := configuration.ConfigCandidates(name, config)
		if err != nil {
			return false, err
		}
		for _, candidate := range candidates {
			if configEnabled(name, candidate) && needsProcess(candidate) {
				return true, nil
			}
		}
		return false, nil
	})
}

func projectMayMatch(config, projectConfig *configuration.Config, match func(*configuration.Config) (bool, error)) (bool, error) {
	if config != nil && config.Lint.Enabled != nil && !*config.Lint.Enabled {
		return false, nil
	}
	possible, err := match(config)
	if possible || err != nil || projectConfig == nil || len(projectConfig.Overrides) == 0 {
		return possible, err
	}
	// A workflow-only override does not apply to a referenced action.yml.
	return match(projectConfig)
}

// A composite's path can match different overrides than its calling workflow.
func mayEnable(name string, config *configuration.Config) (bool, error) {
	candidates, err := configuration.ConfigCandidates(name, config)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(candidates, func(config *configuration.Config) bool { return configEnabled(name, config) }), nil
}

func configEnabled(name string, config *configuration.Config) bool {
	if config == nil {
		return true
	}
	enabled := config.Tools[name].Enabled
	return config.RuleSet().DiagnosticLevel(config, name) != "off" && (enabled == nil || *enabled)
}
