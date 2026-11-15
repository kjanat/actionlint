package scripts

import (
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/integrations/process"
)

// ProjectMayEnable preserves a tool when a calling workflow or composite can enable it.
func ProjectMayEnable(name string, config, projectConfig *configuration.Config) (bool, error) {
	return projectMayMatch(config, projectConfig, func(config *configuration.Config) (bool, error) { return mayEnable(name, config) })
}

// ProjectMayNeedProcess checks possible composite overrides without parsing workflow inputs.
func ProjectMayNeedProcess(name string, config, projectConfig *configuration.Config, command string, explicit *process.CommandOptions) (bool, error) {
	return ProjectMayNeedProcessFor(name, config, projectConfig, func(candidate *configuration.Config) bool {
		return process.CommandEnabled(command, explicit) && Module(name, command, explicit, candidate) == nil
	})
}

// ProjectMayNeedProcessFor resolves a host's process choice for each enabled candidate.
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
	// A workflow-only override does not apply to a referenced action.yml. Keep
	// the baseline available without inventing a second composite graph here.
	return match(projectConfig)
}

// mayEnable reports whether any applicable configuration could enable the tool.
// A composite's path can match different overrides than its calling workflow.
// Tool planning intentionally avoids reading workflows, so preserve a checker
// when an override could enable it. Disabling-only overrides do not turn it on.
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
