package application_test

import (
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/tools/adapters"
)

func TestRequiredToolsFileSelection(t *testing.T) {
	root := t.TempDir()
	config := writeFixture(t, root, "actionlint.yml", `files: {excludes: ['skip.yml']}
overrides:
  - includes: [disabled.yml]
    lint: {rules: {external: {shellcheck: off}}}
`)
	options := application.Options{WorkingDir: root, ConfigFile: config, Tools: (adapters.Options{ShellCheck: "shellcheck"}).Registrations()}
	for _, tc := range []struct {
		path string
		want bool
	}{{"ci.yml", true}, {"skip.yml", false}, {"disabled.yml", false}} {
		got, err := application.RequiredTools(t.Context(), options, []string{tc.path})
		if err != nil || got["shellcheck"] != tc.want {
			t.Fatalf("%s: %+v, %v", tc.path, got, err)
		}
	}
	// Exclusions must apply before trying to read this nonexistent input.
	result, err := application.Check(t.Context(), options, application.Input{Kind: application.Files, Paths: []string{filepath.Join(root, "skip.yml")}})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("excluded file: %+v, %v", result, err)
	}
}
