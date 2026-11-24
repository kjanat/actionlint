// Package pluginacquisition lays out the plugin artifacts, registry records
// and compiled modules in actionlint's dependency cache.
package pluginacquisition

import (
	"os"
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts/oci"
)

func ArtifactCacheDir(acquisition dependencycache.Acquisition) (string, error) {
	return cacheSubdirectory(acquisition, "artifacts")
}

func RegistryCacheDir(acquisition dependencycache.Acquisition) (string, error) {
	return cacheSubdirectory(acquisition, "registry")
}

func ArtifactOptions(acquisition dependencycache.Acquisition) artifacts.Options {
	options := artifacts.Options{PullOCI: oci.Pull, Connections: acquisition.Connections}
	if directory, err := ArtifactCacheDir(acquisition); err == nil {
		options.CacheDir = directory
	}
	return options
}

// CompiledModuleCacheDir returns empty when it cannot create the directory.
func CompiledModuleCacheDir(acquisition dependencycache.Acquisition) string {
	directory, err := cacheSubdirectory(acquisition, "wazero")
	if err != nil {
		return ""
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return ""
	}
	return directory
}

func cacheSubdirectory(acquisition dependencycache.Acquisition, name string) (string, error) {
	directory, err := acquisition.Directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, name), nil
}
