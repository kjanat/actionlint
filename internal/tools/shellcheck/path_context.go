package shellcheck

import (
	"path/filepath"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	shellprofile "actionlint.kjanat.dev/v2/internal/tools/profile/shell"
)

func (run *shellcheckRun) pathContext() repoconfig.PathContext {
	configDir := run.paths.workspace
	if config := run.cfg; config.Path() != "" {
		configDir = filepath.Dir(config.Path())
	}
	return repoconfig.PathContext{ConfigDir: configDir, GitDir: run.paths.workspace, Workspace: shellprofile.RunnerWorkspace(run.paths.workspace), ActionPath: shellprofile.RunnerActionPath(run.actionPath)}
}
