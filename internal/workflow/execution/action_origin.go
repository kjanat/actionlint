package execution

import (
	"path/filepath"
	"strings"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/references"
)

// ActionOrigin resolves the runner and analysis locations of one composite invocation.
func ActionOrigin(paths *Paths, call *workflowlang.Step, actionPath string) {
	paths.ActionPath = actionPath
	paths.ActionRunnerPath, paths.ActionIndependent = "", false
	if action, ok := call.Exec.(*workflowlang.ExecAction); ok && action.Uses != nil {
		_, paths.ActionIndependent = references.SelfRepositoryLocal(action.Uses.Value)
		if !paths.ActionIndependent {
			paths.ActionRunnerPath = strings.TrimPrefix(action.Uses.Value, "./")
			placement := paths.Placements.Matching(paths.ActionRunnerPath)
			if placement != nil && placement.CaseInsensitive && placement.Directory.Kind == DirectoryKnown && !placement.Foreign {
				relative, err := filepath.Rel(paths.Workspace, actionPath)
				if err == nil && filepath.IsLocal(relative) {
					paths.ActionRunnerPath = JoinPath(placement.Directory.Path, filepath.ToSlash(relative))
				}
			}
		}
	}
}
