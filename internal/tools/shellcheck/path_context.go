package shellcheck

import (
	"os"
	"path/filepath"

	repoconfig "actionlint.kjanat.dev/internal/config"
)

func (run *shellcheckRun) pathContext() repoconfig.PathContext {
	configDir := run.paths.workspace
	if config := run.cfg; config.Path() != "" {
		configDir = filepath.Dir(config.Path())
	}
	workspace := os.Getenv("GITHUB_WORKSPACE")
	if workspace == "" {
		workspace = run.paths.workspace
	}
	actionPath := run.actionPath
	if runnerPath := os.Getenv("GITHUB_ACTION_PATH"); actionPath != "" && runnerPath != "" {
		local, localErr := os.Stat(actionPath)
		runner, runnerErr := os.Stat(runnerPath)
		if localErr == nil && runnerErr == nil && os.SameFile(local, runner) {
			actionPath = runnerPath
		}
	}
	return repoconfig.PathContext{ConfigDir: configDir, GitDir: run.paths.workspace, Workspace: workspace, ActionPath: actionPath}
}
