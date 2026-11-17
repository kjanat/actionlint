package composites

import (
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/references/actions/localactions"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
)

func TestCompositeCarriesCaptureAcrossRelativeDisplayPaths(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	root := filepath.Join(base, "repository")
	manifest := filepath.Join(root, "child", "action.yml")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte("name: child\ndescription: child\nruns: {using: composite, steps: [{shell: bash, run: echo relative}]}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, store, fixture := newCapturedSnapshots(t, base)
	path := filepath.Join(base, "ci.yml")
	content := []byte("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/child}]}}\n")
	snapshot, err := fixture.Document(path, content, parsing.Workflow(content))
	if err != nil {
		t.Fatal(err)
	}
	cache := localactions.NewManifestCache(t.Context(), "repository", nil)
	cache.Configure(localactions.Options{Captures: store, Context: t.Context(), WorkingDir: base})
	options := Options{Bindings: testBindings(nil), Context: t.Context(), SourceSnapshots: registry, Actions: localactions.NewResolver(cache, nil), CollectScripts: true, Paths: execution.Paths{Workspace: root, Analysis: base}}
	result, err := Workflow(options, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	scripts := result.Batch.Scripts
	if len(scripts) != 1 || scripts[0].Run.Run.Text() != "echo relative" {
		t.Fatalf("relative action scripts = %+v", scripts)
	}
}
