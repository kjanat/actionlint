// Package workflownames indexes repository workflow names once per analysis.
package workflownames

import (
	"os"
	"path/filepath"
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

// ForRoot includes only workflow files directly under .github/workflows.
// Input-selection filters must not remove potential workflow_run producers.
func (i *Index) ForRoot(root string) (Names, error) {
	i.mu.Lock()
	if i.cache == nil {
		i.cache = map[string]func() (Names, error){}
	}
	load, exists := i.cache[root]
	if !exists {
		load = sync.OnceValues(func() (Names, error) {
			names := Names{Values: map[string]bool{}, Complete: true}
			dir := filepath.Join(root, ".github", "workflows")
			entries, err := os.ReadDir(dir)
			if err != nil {
				return names, err
			}
			paths := map[string]bool{}
			for _, entry := range entries {
				if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".yml") || strings.HasSuffix(entry.Name(), ".yaml")) {
					paths[filepath.Join(dir, entry.Name())] = true
				}
			}
			for _, path := range i.Paths {
				if filepath.Dir(path) == dir && (strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".yaml")) {
					paths[path] = true
				}
			}
			ordered := make([]string, 0, len(paths))
			for path := range paths {
				ordered = append(ordered, path)
			}
			slices.Sort(ordered)
			for _, path := range ordered {
				name, known, err := i.Load(path)
				if err != nil {
					return names, err
				}
				if !known {
					names.Complete = false
					continue
				}
				if name == "" {
					name = ".github/workflows/" + filepath.Base(path)
				}
				names.Values[name] = true
			}
			return names, nil
		})
		i.cache[root] = load
	}
	i.mu.Unlock()
	return load()
}
