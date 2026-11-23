package shell

import "os"

func RunnerWorkspace(repository string) string {
	if workspace := os.Getenv("GITHUB_WORKSPACE"); workspace != "" {
		return workspace
	}
	return repository
}

// RunnerActionPath uses GITHUB_ACTION_PATH only when it names the analyzed
// action's directory.
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
