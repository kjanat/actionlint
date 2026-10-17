package engine

import (
	"context"
	"path"
	"sync"

	"actionlint.kjanat.dev/internal/workspace"
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
		index, err := workspace.ReadIndex(ctx, root)
		snapshot.modes, snapshot.index, snapshot.err = index.Modes, index.Path, err
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
