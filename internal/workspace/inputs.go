package workspace

import (
	"maps"
	"slices"
	"sync"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
)

type Inputs struct {
	mu    sync.Mutex
	paths map[string]struct{}
}

func (f *Inputs) Add(path string) {
	if path == "" || path == "<stdin>" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.paths == nil {
		f.paths = map[string]struct{}{}
	}
	f.paths[absPath(path)] = struct{}{}
}

func (f *Inputs) List() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	paths := make([]string, 0, len(f.paths))
	for path := range f.paths {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths
}

func (f *Inputs) AddConfig(config *repoconfig.Config, project *Project, workingDir string) {
	if config != nil {
		for _, name := range slices.Sorted(maps.Keys(config.Tools)) {
			if module, ok := config.Tools[name].Backend.Wasm(); ok {
				_, local, err := pluginartifact.NormalizeSource(module.Path(), workingDir)
				if err == nil {
					f.Add(local)
				}
			}
		}
		for _, path := range config.Sources() {
			f.Add(path)
		}
	}
	if project != nil {
		f.Add(project.configPath)
	}
}
