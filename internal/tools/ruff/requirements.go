package ruff

import (
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/tools/selection"
)

func projectMayEnable(config, projectConfig *repoconfig.Config, project bool) (bool, error) {
	return selection.ProjectMayEnable("ruff", config, projectConfig, project)
}

func (o Options) projectMayNeedProcess(config, projectConfig *repoconfig.Config, project bool) (bool, error) {
	return selection.ProjectMayNeedProcessFor("ruff", config, projectConfig, project, func(candidate *repoconfig.Config) bool {
		selected := o.forConfig(candidate)
		return selected.Backend == nil && processconfig.Enabled(selected.Command, selected.Process) && selection.Module("ruff", selected.Command, selected.Process, candidate) == nil
	})
}
