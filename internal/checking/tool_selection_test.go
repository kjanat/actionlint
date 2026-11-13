package checking_test

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestRequiredToolsFileSelection(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	config := writeFixture(t, root, "actionlint.yml", `files: {excludes: ['skip.yml']}
overrides:
  - includes: [disabled.yml]
    lint: {rules: {external: {shellcheck: off}}}
`)
	options := checking.Options{WorkingDir: root, ConfigFile: config, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: "shellcheck"}})}
	for _, tc := range []struct {
		path string
		want bool
	}{{"ci.yml", true}, {"skip.yml", true}, {"disabled.yml", false}} {
		got, err := checking.RequiredTools(t.Context(), options, []string{tc.path})
		if err != nil || got["shellcheck"] != tc.want {
			t.Fatalf("%s: %+v, %v", tc.path, got, err)
		}
	}
	// Exclusions must apply before trying to read this nonexistent input.
	path := filepath.Join(root, "skip.yml")
	result, err := checking.Check(t.Context(), options, inputmodel.FromFiles(workspace.FileSelection{Files: []workspace.File{{Path: path}}}))
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("excluded file: %+v, %v", result, err)
	}
	if _, err := checking.Check(t.Context(), options, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{path}}); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("literal file did not override the discovery filter: %v", err)
	}
}
