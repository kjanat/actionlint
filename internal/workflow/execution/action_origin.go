package execution

import (
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	"actionlint.kjanat.dev/v2/internal/workflow/references"
)

// ActionOrigin resolves the runner and analysis locations of one composite invocation.
// An entry has no caller, so only its analysis location is set.
func ActionOrigin(paths *Paths, context invocation.Context, actionPath string) {
	paths.ActionPath = actionPath
	paths.ActionRunnerPath, paths.ActionIndependent = "", false
	effects, ok := context.Facts()
	if !ok || !effects.HasUses {
		return
	}
	_, paths.ActionIndependent = references.SelfRepositoryLocal(effects.Uses)
	if !paths.ActionIndependent {
		paths.ActionRunnerPath = strings.TrimPrefix(effects.Uses, "./")
		placement := paths.Placements.Matching(paths.ActionRunnerPath)
		if placement.Present() && placement.Details().CaseInsensitive && placement.Details().Directory.Kind == DirectoryKnown && !placement.Details().Foreign {
			relative, err := filepath.Rel(paths.Workspace, actionPath)
			if err == nil && filepath.IsLocal(relative) {
				paths.ActionRunnerPath = JoinPath(placement.Details().Directory.Path, filepath.ToSlash(relative))
			}
		}
	}
}
