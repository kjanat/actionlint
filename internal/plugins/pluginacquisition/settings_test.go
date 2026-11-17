package pluginacquisition_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/integrations/acquisitionhttp"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
)

func TestAcquisitionArtifactOptionsUseSelectedUserCache(t *testing.T) {
	t.Parallel()
	selected := t.TempDir()
	connections := acquisitionhttp.Connections{Transport: &http.Transport{}}
	options := pluginacquisition.Options{UserCacheDir: selected, Connections: connections}.ArtifactOptions()
	if options.CacheDir != filepath.Join(selected, "actionlint", "artifacts") || options.Connections != connections || options.PullOCI == nil {
		t.Fatalf("selected artifact options = %+v", options)
	}
	compiled := pluginacquisition.Options{UserCacheDir: selected}.CompiledModuleCacheDir()
	if compiled != filepath.Join(selected, "actionlint", "wazero") {
		t.Fatalf("compiled module cache = %q", compiled)
	}
	if info, err := os.Stat(compiled); err != nil || !info.IsDir() {
		t.Fatalf("compiled module cache not created: %v", err)
	}
}

func TestAcquisitionArtifactOptionsDefaultToHostUserCache(t *testing.T) {
	t.Parallel()
	want := ""
	if base, err := os.UserCacheDir(); err == nil {
		want = filepath.Join(base, "actionlint", "artifacts")
	}
	options := pluginacquisition.Options{}.ArtifactOptions()
	if options.CacheDir != want || options.Connections != (acquisitionhttp.Connections{}) || options.PullOCI == nil {
		t.Fatalf("default artifact options = %+v, want cache %q", options, want)
	}
}
