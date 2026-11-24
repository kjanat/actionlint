package workspace

import (
	"maps"
	"slices"
	"sync"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/configsource"
	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
)

// InputFiles is safe for concurrent use.
type InputFiles struct {
	mu    sync.Mutex
	paths map[string]struct{}
}

func (f *InputFiles) Add(path string) {
	if path == "" || path == "<stdin>" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.paths == nil {
		f.paths = map[string]struct{}{}
	}
	if !configsource.IsRemoteAddress(path) {
		path = absPath(path)
	}
	f.paths[path] = struct{}{}
}

func (f *InputFiles) List() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	paths := make([]string, 0, len(f.paths))
	for path := range f.paths {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths
}

func (f *InputFiles) AddConfig(config *configuration.Config, provenance configuration.Provenance, project *Project, workingDir string) {
	if config != nil {
		for _, name := range slices.Sorted(maps.Keys(config.Tools)) {
			if module, ok := config.Tools[name].Backend.Wasm(); ok {
				// An oci:// reference fails to parse and names no local file.
				if location, err := pluginreference.ParseModuleLocation(module.Path()); err == nil {
					f.Add(location.File(workingDir))
				}
			}
		}
	}
	for _, path := range provenance.Sources() {
		f.Add(path)
	}
	if project != nil {
		f.Add(project.configPath)
	}
}
