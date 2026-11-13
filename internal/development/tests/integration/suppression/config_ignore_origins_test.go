package suppression_test

import (
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
)

func TestReusableIgnoreWorkflowCallOrigin(t *testing.T) {
	t.Parallel()
	const source = `on: push
jobs:
  caller:
    uses: org/repo/.github/workflows/check.yml@main
    with:
      data: '${{ invalid }}'
permissions: {}
`
	findings := testutil.WorkflowDiagnostics(t, source, "ignores: [{rule: expression-reference, uses: org/repo/.github/workflows/check.yml@main}]")
	if len(findings) != 0 {
		t.Fatalf("workflow-call reference lost from job origin: %+v", findings)
	}
}

func TestReusableIgnoresAliasedSourcedShellcheck(t *testing.T) {
	t.Parallel()
	command := testutil.RequireTool(t, "shellcheck")
	root, _ := testutil.ExecutableFixture(t, "shellcheck")
	fixturefiles.Write(t, root, "scripts/check.sh", "echo $VALUE\n")
	const source = `on: push
jobs:
  a:
    runs-on: ubuntu-latest
    steps: &shared
      - run: . ./scripts/check.sh
        shell: bash
        id: validate
  b:
    runs-on: ubuntu-latest
    steps: *shared
permissions: {}
`
	for _, tc := range []struct {
		ignores string
		want    int
	}{
		{"[{rule: shellcheck, job: a, step: validate}]", 1},
		{"[{rule: shellcheck, job: b, step: validate}]", 1},
		{"[{rule: shellcheck, job: a}, {rule: shellcheck, job: b}]", 0},
	} {
		config, err := checking.ConfigurationRules().Parse([]byte("tools: {shellcheck: true, ruff: false}\nignores: " + tc.ignores))
		if err != nil {
			t.Fatal(err)
		}
		result, err := checking.Analyze(t.Context(), checking.Options{
			WorkingDir: root,

			Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: command, Process: &processconfig.Options{Arguments: []string{"--check-sourced"}}}}),
		}, []workspace.Source{{Path: filepath.Join(root, ".github/workflows/ci.yml"), Content: []byte(source), Config: config, Project: workspace.Identity(root)}},
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Diagnostics) != tc.want {
			t.Fatalf("ignores %s: %+v", tc.ignores, result.Diagnostics)
		}
	}
}
