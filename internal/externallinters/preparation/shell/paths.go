package shell

import "os"

// RunnerWorkspace is the workspace directory a step sees: the runner's
// GITHUB_WORKSPACE, or the analyzed repository when run locally.
func RunnerWorkspace(repository string) string {
	if workspace := os.Getenv("GITHUB_WORKSPACE"); workspace != "" {
		return workspace
	}
	return repository
}

// RunnerActionPath is the action directory a step sees. GITHUB_ACTION_PATH
// replaces the analyzed action's directory only when both name the same
// directory; ambient runner context never invents an action.
func RunnerActionPath(actionPath string) string {
	runnerPath := os.Getenv("GITHUB_ACTION_PATH")
	if actionPath == "" || runnerPath == "" {
		return actionPath
	}
	local, localErr := os.Stat(actionPath)
	runner, runnerErr := os.Stat(runnerPath)
	if localErr == nil && runnerErr == nil && os.SameFile(local, runner) {
		return runnerPath
	}
	return actionPath
}
