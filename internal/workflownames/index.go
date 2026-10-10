// Package workflownames indexes repository workflow names once per analysis.
package workflownames

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
)

// Names is a complete inventory only when every workflow name could be read.
type Names struct {
	Values   map[string]bool
	Complete bool
}

// Index shares immutable inventories across concurrent workflow checks.
type Index struct {
	Load func(string) (name string, known bool, err error)
	// Paths includes in-memory source files which may not exist on disk yet.
	Paths []string
	mu    sync.Mutex
	cache map[string]func() (Names, error)
}

// PathKey identifies inventory paths while retaining platform case semantics.
func PathKey(path string) string {
	return pathKey(path, runtime.GOOS == "windows")
}

func pathKey(path string, windows bool) string {
	path = filepath.Clean(path)
	if windows {
		path = strings.ToLower(path)
	}
	return path
}

func workflowPaths(dir string, candidates []string, windows bool) []string {
	dir = pathKey(dir, windows)
	paths := map[string]string{}
	for _, path := range candidates {
		key := pathKey(path, windows)
		if filepath.Dir(key) == dir && (strings.HasSuffix(key, ".yml") || strings.HasSuffix(key, ".yaml")) {
			paths[key] = path
		}
	}
	ordered := make([]string, 0, len(paths))
	for _, path := range paths {
		ordered = append(ordered, path)
	}
	slices.Sort(ordered)
	return ordered
}

// ForRoot includes only workflow files directly under .github/workflows.
// Input-selection filters must not remove potential workflow_run producers.
func (i *Index) ForRoot(root string) (Names, error) {
	i.mu.Lock()
	if i.cache == nil {
		i.cache = map[string]func() (Names, error){}
	}
	key := PathKey(root)
	load, exists := i.cache[key]
	if !exists {
		load = sync.OnceValues(func() (Names, error) {
			names := Names{Values: map[string]bool{}, Complete: true}
			dir := filepath.Join(root, ".github", "workflows")
			entries, err := os.ReadDir(dir)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				names.Complete = false
			}
			var candidates []string
			diskNames := map[string]string{}
			for _, entry := range entries {
				if !entry.IsDir() {
					path := filepath.Join(dir, entry.Name())
					candidates = append(candidates, path)
					diskNames[PathKey(path)] = entry.Name()
				}
			}
			candidates = append(candidates, i.Paths...)
			for _, path := range workflowPaths(dir, candidates, runtime.GOOS == "windows") {
				name, known, err := i.Load(path)
				if err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return names, err
					}
					names.Complete = false
					continue
				}
				if !known {
					names.Complete = false
					continue
				}
				if name == "" {
					filename, exists := diskNames[PathKey(path)]
					if !exists {
						filename = filepath.Base(path)
					}
					name = ".github/workflows/" + filename
				}
				names.Values[name] = true
			}
			return names, nil
		})
		i.cache[key] = load
	}
	i.mu.Unlock()
	return load()
}
