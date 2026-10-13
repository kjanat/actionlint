package actionlint

import (
	"context"
	"fmt"
	"path"
	"sync"

	"actionlint.kjanat.dev/internal/gitrepo"
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
	inputs   []string
	err      error
}

func (cache *gitModes) load(_ context.Context, root string) *gitModeSnapshot {
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
		repo, err := gitrepo.Open(root)
		if err != nil {
			snapshot.err = fmt.Errorf("locate Git index: %w", err)
			return
		}
		if repo == nil {
			snapshot.err = fmt.Errorf("%s is outside a Git repository", root)
			return
		}
		if repo.WorkTree == "" {
			snapshot.err = fmt.Errorf("%s is in a Git repository without a work tree", root)
			return
		}
		entries, err := repo.ExpandedIndex()
		if err != nil {
			snapshot.err = fmt.Errorf("read Git index: %w", err)
			return
		}
		snapshot.index = repo.IndexPath()
		snapshot.inputs = repo.ReadFiles()
		snapshot.modes = gitModeMap(entries)
	})
	return snapshot
}

// An intent-to-add entry is absent from the prospective tree, including a
// HEAD path re-added with -N.
func gitModeMap(entries []gitrepo.IndexEntry) map[string]string {
	modes := make(map[string]string, len(entries))
	intent := make(map[string]struct{})
	for _, entry := range entries {
		switch {
		case entry.Stage != 0:
			modes[entry.Path] = "unmerged"
		case entry.IntentToAdd:
			intent[entry.Path] = struct{}{}
		case modes[entry.Path] != "unmerged":
			modes[entry.Path] = fmt.Sprintf("%06o", entry.Mode)
		}
	}
	for name := range intent {
		if modes[name] != "unmerged" {
			delete(modes, name)
		}
	}
	return modes
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
