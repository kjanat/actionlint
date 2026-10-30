package composition

import (
	"os"
	"path/filepath"
	"testing"

	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
)

func TestCompositeCarriesCaptureAcrossRelativeDisplayPaths(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repository")
	manifest := filepath.Join(root, "child", "action.yml")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte("name: child\ndescription: child\nruns: {using: composite, steps: [{shell: bash, run: echo relative}]}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry := newCapturedSnapshots(t, base)
	path := filepath.Join(base, "ci.yml")
	content := []byte("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/child}]}}\n")
	snapshot, err := registry.Document(path, content, documentparse.Workflow(path, content))
	if err != nil {
		t.Fatal(err)
	}
	cache := actionlocal.NewCache(t.Context(), "repository", nil)
	cache.Configure(actionlocal.Options{Captures: registry.Sources.Store, Context: t.Context(), WorkingDir: base})
	options := Options{Context: t.Context(), SourceSnapshots: registry.SourceSnapshots, Actions: actionlocal.NewResolver(cache, nil), CollectScripts: true, Paths: execution.Paths{Workspace: root, Analysis: base}}
	result, err := Workflow(options, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	scripts := result.Batch.Scripts()
	if len(scripts) != 1 || scripts[0].Run.Run().Value() != "echo relative" {
		t.Fatalf("relative action scripts = %+v", scripts)
	}
}
