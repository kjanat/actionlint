package suppression_test

import (
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestReusableIgnoreWorkflowCallOrigin(t *testing.T) {
	const source = `on: push
jobs:
  caller:
    uses: org/repo/.github/workflows/check.yml@main
    with:
      data: '${{ invalid }}'
`
	findings := testutil.WorkflowDiagnostics(t, source, "ignores: [{rule: expression-reference, uses: org/repo/.github/workflows/check.yml@main}]")
	if len(findings) != 0 {
		t.Fatalf("workflow-call reference lost from job origin: %+v", findings)
	}
}

func TestReusableIgnoresAliasedSourcedShellcheck(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	root, _ := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, "scripts/check.sh", "echo $VALUE\n")
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
`
	for _, tc := range []struct {
		ignores string
		want    int
	}{
		{"[{rule: shellcheck, job: a, step: validate}]", 1},
		{"[{rule: shellcheck, job: b, step: validate}]", 1},
		{"[{rule: shellcheck, job: a}, {rule: shellcheck, job: b}]", 0},
	} {
		config, err := repoconfig.Parse([]byte("tools: {shellcheck: true, ruff: false}\nignores: " + tc.ignores))
		if err != nil {
			t.Fatal(err)
		}
		result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root,

			Tools: (adapters.Options{ShellCheck: command,
				ShellcheckOptions: &processconfig.Options{Arguments: []string{"--check-sourced"}}}).Registrations()}, []workspace.Source{{Path: filepath.Join(root, ".github/workflows/ci.yml"), Content: []byte(source), Config: config, Project: workspace.Identity(root)}},
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Diagnostics) != tc.want {
			t.Fatalf("ignores %s: %+v", tc.ignores, result.Diagnostics)
		}
	}
}
