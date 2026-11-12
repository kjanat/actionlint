package tools_test

import (
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
)

func TestCompositeShellcheckSourcedDiagnostics(t *testing.T) {
	t.Parallel()
	command := testutil.RequireTool(t, "shellcheck")
	root, _ := testutil.ExecutableFixture(t, "shellcheck")
	fixturefiles.Write(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	path := fixturefiles.Write(t, root, "app/lib/check.sh", "echo $VALUE\n")
	fixturefiles.Write(t, root, "local/action.yml", `name: test
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: app
      run: . ./lib/check.sh
`)
	result := testutil.Composite(t, root, "- uses: ./local", checking.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: command, Process: &processconfig.Options{Arguments: []string{"-a"}}}})})
	if len(result.Diagnostics) != 1 {
		t.Fatalf("want one sourced finding: %+v", result.Diagnostics)
	}
	finding := result.Diagnostics[0]
	if want := filepath.Join("app", "lib", "check.sh"); finding.Path != want {
		t.Fatalf("want relative diagnostic path %q, got %q", want, finding.Path)
	}
	testutil.AssertSourceFile(t, filepath.Join(root, finding.Path), path)
	if finding.Start != (sourcepos.Position{Line: 1, Column: 6}) || finding.Snippet != "echo $VALUE" {
		t.Fatalf("sourced finding attributed to composite: %+v", finding)
	}
}

func TestCompositeShellcheckSiblingWorkingDirectory(t *testing.T) {
	t.Parallel()
	command := testutil.RequireTool(t, "shellcheck")
	root, _ := testutil.ExecutableFixture(t, "shellcheck")
	fixturefiles.Write(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	fixturefiles.Write(t, filepath.Dir(root), "shared/value.sh", "VALUE=42\n")
	fixturefiles.Write(t, root, "local/action.yml", `name: test
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: ../shared
      run: |
        . ./value.sh
        echo $VALUE
`)
	result := testutil.Composite(t, root, "- uses: ./local", checking.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: command}})})
	if len(result.Diagnostics) != 0 {
		t.Fatalf("composite did not analyze sibling source: %+v", result.Diagnostics)
	}
}
