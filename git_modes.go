package actionlint

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/execabs"
)

// Each analysis gets fresh Git index snapshots.
type gitModes struct {
	mu      sync.Mutex
	entries map[string]*gitModeSnapshot
}

type gitModeSnapshot struct {
	once     sync.Once
	dirsOnce sync.Once
	foldOnce sync.Once
	modes    map[string]string
	dirs     map[string]struct{}
	folded   map[string]string
	index    string
	err      error
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
		index, err := repositoryGit(ctx, git, root, "rev-parse", "--git-path", "index").Output()
		if err != nil {
			snapshot.err = fmt.Errorf("locate Git index: %w", err)
			return
		}
		location := strings.TrimSuffix(strings.TrimSuffix(string(index), "\n"), "\r")
		if location == "" || strings.ContainsAny(location, "\x00\r\n") {
			snapshot.err = fmt.Errorf("unexpected Git index location %q", index)
			return
		}
		if !filepath.IsAbs(location) {
			location = filepath.Join(root, location)
		}
		snapshot.index, snapshot.err = filepath.Abs(location)
	})
	return snapshot
}

// Modes are immutable after loading. Share the derived directory and folding
// indexes across all invocations, including those using different checkout paths.
func (snapshot *gitModeSnapshot) prepareDirectories() {
	snapshot.dirsOnce.Do(func() {
		snapshot.dirs = make(map[string]struct{})
		for name := range snapshot.modes {
			for directory := path.Dir(name); directory != "."; directory = path.Dir(directory) {
				if _, exists := snapshot.dirs[directory]; exists {
					break // Its ancestors were added with the first descendant.
				}
				snapshot.dirs[directory] = struct{}{}
			}
		}
	})
}

func (snapshot *gitModeSnapshot) prepareFoldedPaths() {
	snapshot.foldOnce.Do(func() {
		snapshot.prepareDirectories()
		snapshot.folded = make(map[string]string)
		add := func(prefix string) {
			key := pathFoldKey(prefix)
			canonical, exists := snapshot.folded[key]
			if !asciiPath(prefix) || exists && canonical != prefix {
				snapshot.folded[key] = "" // Ambiguous or unsupported normalization.
			} else {
				snapshot.folded[key] = prefix
			}
		}
		for name := range snapshot.modes {
			add(name)
		}
		for directory := range snapshot.dirs {
			add(directory)
		}
	})
}

func repositoryGit(ctx context.Context, git, root string, args ...string) *exec.Cmd {
	// Index inspection must not invoke repository-configured fsmonitor commands.
	command := exec.CommandContext(ctx, git, append([]string{"-C", root, "-c", "core.fsmonitor=false"}, args...)...)
	// Clear repository overrides inherited from Git commands or hooks so the
	// query uses the workflow's project.
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
