package ruff

import (
	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/tools/selection"
)

func projectMayEnable(config, projectConfig *repoconfig.Config, project bool) (bool, error) {
	return selection.ProjectMayEnable("ruff", config, projectConfig, project)
}

func (o Options) projectMayNeedProcess(config, projectConfig *repoconfig.Config, project bool) (bool, error) {
	if o.Backend != nil {
		return false, nil
	}
	return selection.ProjectMayNeedProcess("ruff", config, projectConfig, project, o.Command, o.Process)
}
