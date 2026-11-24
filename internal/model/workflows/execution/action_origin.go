package execution

import (
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/calls/callreferences"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
)

// ActionOrigin sets only the analysis location for an entry, which has no caller.
func ActionOrigin(paths *Paths, context invocation.Context, actionPath string) {
	paths.ActionPath = actionPath
	paths.ActionRunnerPath, paths.ActionIndependent = "", false
	effects, ok := context.Facts()
	if !ok || !effects.HasUses {
		return
	}
	_, paths.ActionIndependent = callreferences.SelfRepositoryLocal(effects.Uses)
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
