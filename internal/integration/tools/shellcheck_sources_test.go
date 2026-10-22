package tools_test

import (
	"path/filepath"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
)

func TestCompositeShellcheckSourcedDiagnostics(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	root, _ := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	path := testutil.WriteFixture(t, root, "app/lib/check.sh", "echo $VALUE\n")
	testutil.WriteFixture(t, root, "local/action.yml", `name: test
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: app
      run: . ./lib/check.sh
`)
	result := testutil.Composite(t, root, "- uses: ./local", application.Options{Tools: (adapters.Options{ShellCheck: command, ShellcheckOptions: &processconfig.Options{Arguments: []string{"-a"}}}).Registrations()})
	if len(result.Diagnostics) != 1 {
		t.Fatalf("want one sourced finding: %+v", result.Diagnostics)
	}
	finding := result.Diagnostics[0]
	if want := filepath.Join("app", "lib", "check.sh"); finding.Path != want {
		t.Fatalf("want relative diagnostic path %q, got %q", want, finding.Path)
	}
	testutil.AssertSourceFile(t, filepath.Join(root, finding.Path), path)
	if finding.Start != (diagmodel.Position{Line: 1, Column: 6}) || finding.Snippet != "echo $VALUE" {
		t.Fatalf("sourced finding attributed to composite: %+v", finding)
	}
}

func TestCompositeShellcheckSiblingWorkingDirectory(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	root, _ := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	testutil.WriteFixture(t, filepath.Dir(root), "shared/value.sh", "VALUE=42\n")
	testutil.WriteFixture(t, root, "local/action.yml", `name: test
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
	result := testutil.Composite(t, root, "- uses: ./local", application.Options{Tools: (adapters.Options{ShellCheck: command}).Registrations()})
	if len(result.Diagnostics) != 0 {
		t.Fatalf("composite did not analyze sibling source: %+v", result.Diagnostics)
	}
}
