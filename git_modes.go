package actionlint

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"golang.org/x/sys/execabs"
)

// Index snapshots live for one analysis, not for the lifetime of a session.
type gitModes struct {
	mu      sync.Mutex
	entries map[string]*gitModeSnapshot
}

type gitModeSnapshot struct {
	once  sync.Once
	modes map[string]string
	index string
	err   error
}

func (cache *gitModes) load(ctx context.Context, root string) *gitModeSnapshot {
	cache.mu.Lock()
	if cache.entries == nil {
		cache.entries = make(map[string]*gitModeSnapshot)
	}
	snapshot := cache.entries[root]
	if snapshot == nil {
		snapshot = &gitModeSnapshot{}
		cache.entries[root] = snapshot
	}
	cache.mu.Unlock()
	snapshot.once.Do(func() {
		git, err := execabs.LookPath("git")
		if err != nil {
			snapshot.err = err
			return
		}
		output, err := repositoryGit(ctx, git, root, "ls-files", "--stage", "-z").Output()
		if err != nil {
			snapshot.err = fmt.Errorf("read Git index: %w", err)
			return
		}
		snapshot.modes, snapshot.err = parseGitModes(string(output))
		if snapshot.err != nil {
			return
		}
		index, err := repositoryGit(ctx, git, root, "rev-parse", "--path-format=absolute", "--git-path", "index").Output()
		if err != nil {
			snapshot.err = fmt.Errorf("locate Git index: %w", err)
			return
		}
		snapshot.index = strings.TrimSuffix(strings.TrimSuffix(string(index), "\n"), "\r")
	})
	return snapshot
}

func repositoryGit(ctx context.Context, git, root string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, git, append([]string{"-C", root}, args...)...)
	// Parent Git commands and hooks can export repository overrides. This query
	// belongs to the workflow's project, not whichever repository invoked us.
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(name) {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR":
			continue
		}
		command.Env = append(command.Env, entry)
	}
	return command
}

func parseGitModes(output string) (map[string]string, error) {
	modes := make(map[string]string)
	for record := range strings.SplitSeq(output, "\x00") {
		if record == "" {
			continue
		}
		metadata, name, ok := strings.Cut(record, "\t")
		fields := strings.Fields(metadata)
		if !ok || len(fields) != 3 || name == "" {
			return nil, fmt.Errorf("unexpected Git index entry %q", record)
		}
		if fields[2] != "0" {
			modes[name] = "unmerged"
		} else if modes[name] != "unmerged" {
			modes[name] = fields[0]
		}
	}
	return modes, nil
}
