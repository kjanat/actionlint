package ruff

import (
	"context"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/tools/backend"
)

func projectMayEnable(config, projectConfig *repoconfig.Config, project bool) (bool, error) {
	return backend.ProjectMayEnable("ruff", config, projectConfig, project)
}

func (o Options) projectMayNeedProcess(_ context.Context, config, projectConfig *repoconfig.Config, project bool) (bool, error) {
	return backend.ProjectMayNeedProcessFor("ruff", config, projectConfig, project, func(candidate *repoconfig.Config) bool {
		selected := o.forConfig(candidate)
		return selected.Backend == nil && processconfig.Enabled(selected.Command, selected.Process) && backend.Module("ruff", selected.Command, selected.Process, candidate) == nil
	})
}
