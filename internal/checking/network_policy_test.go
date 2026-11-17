package checking

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestNetworkPolicyAppliesToPreparedSources(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "policy.toml")
	if err := os.WriteFile(path, []byte("[restrictions.network.metadata-mirrors]\nexclude-services = ['jsdelivr']\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACTIONLINT_NETWORK_POLICY", path)
	requests := &mirrorRequests{actions: map[references.MirrorAccess]int{}, workflows: map[references.MirrorAccess]int{}}
	options := Options{WorkingDir: root, SkipProjectConfig: true, RemoteActions: mirrorProvider{requests: requests}}
	sources := []workspace.InputFile{{Path: filepath.Join(root, "workflow.yml"), Content: []byte("on: push\npermissions: {}\njobs: {build: {runs-on: ubuntu-latest, steps: [{uses: example/action@v1}]}}\n")}}
	if _, err := Analyze(t.Context(), options, sources); err != nil {
		t.Fatal(err)
	}
	if requests.actions[references.NoMirrors] != 1 || len(requests.actions) != 1 {
		t.Fatalf("requests by mirror setting = %v, want one with mirrors disabled", requests.actions)
	}
	services := configuration.MetadataMirrorServices{"jsdelivr"}
	sources[0].Config = &configuration.Config{Network: configuration.NetworkConfig{MetadataMirrors: configuration.MetadataMirrorsConfig{Services: &services}}}
	if _, err := Analyze(t.Context(), options, sources); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("prepared config bypassed policy: %v", err)
	}
}
