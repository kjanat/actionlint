package installation_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

func TestAcquisitionArtifactOptionsUseSelectedUserCache(t *testing.T) {
	t.Parallel()
	selected := t.TempDir()
	connections := acquisitionhttp.Connections{Transport: &http.Transport{}}
	options := plugininstall.Acquisition{UserCacheDir: selected, Connections: connections}.ArtifactOptions()
	if options.CacheDir != filepath.Join(selected, "actionlint", "artifacts") || options.Connections != connections || options.PullOCI == nil {
		t.Fatalf("selected artifact options = %+v", options)
	}
	compiled := plugininstall.Acquisition{UserCacheDir: selected}.CompiledModuleCacheDir()
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
	options := plugininstall.Acquisition{}.ArtifactOptions()
	if options.CacheDir != want || options.Connections != (acquisitionhttp.Connections{}) || options.PullOCI == nil {
		t.Fatalf("default artifact options = %+v, want cache %q", options, want)
	}
}
