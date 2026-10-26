package execution

import (
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/v2/internal/workflow/references"
)

// ActionOrigin resolves the runner and analysis locations of one composite invocation.
// An entry has no caller, so only its analysis location is set.
func ActionOrigin(paths *Paths, context ActionContext, actionPath string) {
	paths.ActionPath = actionPath
	paths.ActionRunnerPath, paths.ActionIndependent = "", false
	effects, ok := context.Effects()
	if !ok || !context.hasUses {
		return
	}
	_, paths.ActionIndependent = references.SelfRepositoryLocal(effects.Uses)
	if !paths.ActionIndependent {
		paths.ActionRunnerPath = strings.TrimPrefix(effects.Uses, "./")
		placement := paths.Placements.Matching(paths.ActionRunnerPath)
		if placement != nil && placement.CaseInsensitive && placement.Directory.Kind == DirectoryKnown && !placement.Foreign {
			relative, err := filepath.Rel(paths.Workspace, actionPath)
			if err == nil && filepath.IsLocal(relative) {
				paths.ActionRunnerPath = JoinPath(placement.Directory.Path, filepath.ToSlash(relative))
			}
		}
	}
}
