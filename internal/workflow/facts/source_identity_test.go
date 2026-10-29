package facts_test

import (
	"strings"
	"testing"

	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"
	"actionlint.kjanat.dev/v2/source"
)

func TestAnonymousStepIdentitySurvivesReordering(t *testing.T) {
	for _, tc := range []struct{ name, first, second string }{
		{"env", "run: echo hello\n  env: {FIRST: value}", "run: echo hello\n  env: {SECOND: value}"},
		{"inputs", "uses: owner/action@main\n  with: {key: first}", "uses: owner/action@main\n  with: {key: second}"},
		{"condition", "run: echo hello\n  if: success()", "run: echo hello\n  if: failure()"},
		{"directory", "run: echo hello\n  working-directory: first", "run: echo hello\n  working-directory: second"},
		{"timeout", "run: echo hello\n  timeout-minutes: 1", "run: echo hello\n  timeout-minutes: 2"},
		{"continue", "run: echo hello\n  continue-on-error: true", "run: echo hello\n  continue-on-error: false"},
		{"background", "run: echo hello\n  background: true", "run: echo hello\n  background: false"},
		{"insertions", "run: echo hello\n  env:\n    '${{ insert }}': {KEY: first}", "run: echo hello\n  env:\n    '${{ insert }}': {KEY: second}"},
		{"parallel", "parallel:\n    - run: echo first", "parallel:\n    - run: echo second"},
		{"wait", "wait: first", "wait: second"},
		{"cancel", "cancel: first", "cancel: second"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := stepReferencesForTest(t, tc.first, tc.second)
			after := stepReferencesForTest(t, tc.second, tc.first)
			if before[0].Node == before[1].Node || before[0].Node != after[1].Node || before[1].Node != after[0].Node {
				t.Fatalf("identities followed positions: before=%+v after=%+v", before, after)
			}
			if before[0].Address == after[1].Address {
				t.Fatal("structural address did not follow the move")
			}
			if tc.name == "parallel" && (before[2].Node != after[3].Node || before[3].Node != after[2].Node) {
				t.Fatal("parallel children lost their owning identity")
			}
		})
	}
}

func TestStepIdentityIgnoresPresentation(t *testing.T) {
	before := stepReferencesForTest(t, "run: echo hello\n  env: {FIRST: one, SECOND: two}")
	after := stepReferencesForTest(t, "# comment\n  run: 'echo hello'\n  env: {SECOND: two, FIRST: one}")
	if before[0].Node != after[0].Node {
		t.Fatal("comment, quoting, or mapping order changed identity")
	}
}

func TestAliasedCallReferencesFollowJobAndStep(t *testing.T) {
	const first = "on: push\njobs:\n  Build:\n    runs-on: ubuntu-latest\n    steps:\n      - &shared\n        uses: $/outer\n"
	for _, test := range []struct {
		name, suffix, secondJob, secondAddress string
		secondStep                             int
	}{
		{name: "same job", suffix: "      - *shared\n", secondJob: "build", secondStep: 1, secondAddress: `jobs["build"].steps[1]`},
		{name: "different jobs", suffix: "  Other:\n    runs-on: ubuntu-latest\n    steps:\n      - *shared\n", secondJob: "other", secondAddress: `jobs["other"].steps[0]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := []byte(first + test.suffix)
			workflow, findings := workflowparser.ParseDocument(content)
			if len(findings) != 0 {
				t.Fatal(findings)
			}
			first := workflow.Jobs["build"].Steps[0]
			second := workflow.Jobs[test.secondJob].Steps[test.secondStep]
			refs := workflowfacts.Index("ci.yml", content, workflow, false)
			left, right := refs.Steps[first], refs.Steps[second]
			if left.Node == "" || right.Node == "" || left.Node == right.Node {
				t.Fatalf("aliased calls lost their owners: first=%+v second=%+v", left, right)
			}
			if left.Address != `jobs["build"].steps[0]` || right.Address != test.secondAddress {
				t.Fatalf("owning addresses = %q, %q", left.Address, right.Address)
			}
			repeated := workflowfacts.Index("ci.yml", content, workflow, false)
			if repeated.Steps[first] != left || repeated.Steps[second] != right {
				t.Fatal("declaration identities changed across indexing")
			}
		})
	}
}

func stepReferencesForTest(t *testing.T, steps ...string) []source.Reference {
	t.Helper()
	var text strings.Builder
	text.WriteString("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n")
	for _, step := range steps {
		text.WriteString("      - ")
		text.WriteString(strings.ReplaceAll(step, "\n", "\n      "))
		text.WriteString("\n")
	}
	content := text.String()
	workflow, findings := workflowparser.ParseDocument([]byte(content))
	if len(findings) != 0 {
		t.Fatal(findings)
	}
	refs := workflowfacts.Index("workflow.yml", []byte(content), workflow, false)
	var result []source.Reference
	for _, step := range workflow.Jobs["build"].Steps {
		result = append(result, refs.Steps[step])
	}
	for _, step := range workflow.Jobs["build"].Steps {
		if parallel, ok := step.Exec.(*workflowlang.ExecParallel); ok {
			for _, child := range parallel.Steps {
				result = append(result, refs.Steps[child])
			}
		}
	}
	return result
}
