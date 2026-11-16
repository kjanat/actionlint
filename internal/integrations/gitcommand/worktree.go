package gitcommand

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Worktree is the commit a repository's HEAD names and the paths that differ
// from it in the index or working tree.
type Worktree struct {
	// Head is the full commit ID HEAD names. It is empty before the first commit.
	Head string
	// Changes lists modified, added, deleted, renamed and untracked paths
	// relative to the repository root, with forward slashes, in Git's order.
	// Ignored files are not listed.
	Changes []string
}

// Dirty reports whether any path differs from HEAD.
func (w Worktree) Dirty() bool { return len(w.Changes) != 0 }

// ReadWorktree reads HEAD and the uncommitted changes of the repository at
// root with two local Git queries. It does not fetch, prompt or run the
// repository's fsmonitor.
func ReadWorktree(ctx context.Context, root string) (Worktree, error) {
	if err := ctx.Err(); err != nil {
		return Worktree{}, err
	}
	git, err := executable()
	if err != nil {
		return Worktree{}, err
	}
	var worktree Worktree
	head, err := Query(ctx, git, root, nil, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if exit, ok := errors.AsType[*exec.ExitError](err); ok && exit.ExitCode() == 1 && len(head.Stdout) == 0 {
		// --verify --quiet exits 1 without output when HEAD names no commit yet.
		err = nil
	}
	if err != nil {
		return Worktree{}, fmt.Errorf("read Git HEAD: %w", err)
	}
	worktree.Head = strings.TrimSpace(string(head.Stdout))
	status, err := Query(ctx, git, root, nil, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=dirty")
	if err != nil {
		return Worktree{}, fmt.Errorf("read Git status: %w", err)
	}
	worktree.Changes, err = ParseStatusPaths(string(status.Stdout))
	return worktree, err
}

// ParseStatusPaths reads the paths of `git status --porcelain=v1 -z` output.
// For a rename or copy it returns the new path and skips the original one.
func ParseStatusPaths(output string) ([]string, error) {
	var paths []string
	records := strings.Split(output, "\x00")
	for i := 0; i < len(records); i++ {
		record := records[i]
		if record == "" {
			continue
		}
		if len(record) < 4 || record[2] != ' ' {
			return nil, fmt.Errorf("unexpected Git status entry %q", record)
		}
		paths = append(paths, record[3:])
		// The index or the worktree column can mark a rename or copy.
		if strings.ContainsAny(record[:2], "RC") {
			i++
		}
	}
	return paths, nil
}
