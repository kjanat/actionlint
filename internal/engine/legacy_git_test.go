package engine

import (
	"context"
	"os/exec"

	"actionlint.kjanat.dev/internal/workspace"
)

func repositoryGit(ctx context.Context, git, root string, args ...string) *exec.Cmd {
	return workspace.GitCommand(ctx, git, root, args...)
}
func parseGitModes(output string) (map[string]string, error) {
	return workspace.ParseIndexModes(output)
}
