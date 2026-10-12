package actionlint

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestReusableIgnoresDistinguishAliasedSteps(t *testing.T) {
	const source = `on: push
jobs:
  a:
    runs-on: ubuntu-latest
    steps: &shared
      - id: validate
        run: echo '${{ invalid }}'
  b:
    runs-on: ubuntu-latest
    steps: *shared
`
	for _, job := range []string{"a", "b"} {
		for _, source := range []string{source, strings.NewReplacer("steps: &shared\n      - id:", "steps:\n      - &shared\n        id:", "steps: *shared", "steps:\n      - *shared").Replace(source)} {
			findings := lintCachePolicy(t, source, "ignores: [{rule: expression, job: "+job+", step: validate}]")
			if len(findings) != 1 || findings[0].Kind != "expression" || findings[0].ignoreOrigin == nil || findings[0].ignoreOrigin.job == job {
				t.Fatalf("ignore job %s did not retain other invocation: %+v", job, findings)
			}
		}
	}
}

func TestReusableIgnoresDistinguishAliasedActionSteps(t *testing.T) {
	const source = `on: push
jobs:
  a:
    runs-on: ubuntu-latest
    steps: &shared
      - id: validate
        uses: invalid
  b:
    runs-on: ubuntu-latest
    steps: *shared
`
	findings := lintCachePolicy(t, source, "ignores: [{rule: action, job: a, uses: invalid}]")
	if len(findings) != 1 || findings[0].Kind != "action" || findings[0].ignoreOrigin == nil || findings[0].ignoreOrigin.job != "b" {
		t.Fatalf("lost alias invocation: %+v", findings)
	}
}

func TestReusableIgnoreWorkflowCallOrigin(t *testing.T) {
	const source = `on: push
jobs:
  caller:
    uses: org/repo/.github/workflows/check.yml@main
    with:
      data: '${{ invalid }}'
`
	findings := lintCachePolicy(t, source, "ignores: [{rule: expression, uses: org/repo/.github/workflows/check.yml@main}]")
	if len(findings) != 0 {
		t.Fatalf("workflow-call reference lost from job origin: %+v", findings)
	}
}

func TestReusableIgnoresAliasedSourcedShellcheck(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, "scripts/check.sh", "echo $VALUE\n")
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
		config, err := ParseConfig([]byte("tools: {shellcheck: true, ruff: false}\nignores: " + tc.ignores))
		if err != nil {
			t.Fatal(err)
		}
		result, err := Analyze(t.Context(), AnalysisRequest{
			WorkingDir: root, ShellCheck: command,
			ShellcheckOptions: &ExternalCommandOptions{Arguments: []string{"--check-sourced"}},
			Sources:           []SourceUnit{{Path: filepath.Join(root, ".github/workflows/ci.yml"), Content: []byte(source), Config: config, Project: &Project{root: root}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Diagnostics) != tc.want {
			t.Fatalf("ignores %s: %+v", tc.ignores, result.Diagnostics)
		}
	}
}
