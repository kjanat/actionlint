package workflow_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/testutil"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/matrix"
	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

func repositoryWorkflowSource(t *testing.T, name string) []byte {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("internal", "integration", "workflow", "testdata", "repositories", name+".yml"))
	if os.IsNotExist(err) && (name == "runny-conditions" || name == "runny-matrix-insertion") {
		t.Skip("local fixture from private repository is unavailable")
	}
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func parseRepositoryWorkflow(t *testing.T, name string) *workflowlang.Workflow {
	t.Helper()
	workflow, findings := workflowparser.ParseDocument(repositoryWorkflowSource(t, name))
	if len(findings) != 0 || workflow == nil {
		t.Fatalf("repository workflow rejected: %v", findings)
	}
	return workflow
}

func TestRepositoryWorkflowSyntax(t *testing.T) {
	for _, name := range []string{
		"actions-rs", "actup-autofix", "shellcheck-wiki", "runny-matrix-insertion",
		"runny-conditions", "runner-release", "svg-release", "vw-release",
	} {
		t.Run(name, func(t *testing.T) { parseRepositoryWorkflow(t, name) })
	}
}

func TestRepositoryWorkflowAnalysis(t *testing.T) {
	for _, name := range []string{
		"actup-autofix", "shellcheck-wiki", "runny-matrix-insertion",
		"runner-release", "svg-release", "vw-release",
	} {
		t.Run(name, func(t *testing.T) {
			findings := testutil.WorkflowDiagnostics(t, string(repositoryWorkflowSource(t, name)), "repository: {runners: []}\ntools: {shellcheck: false, ruff: false}")
			if len(findings) != 0 {
				t.Fatalf("unexpected repository workflow findings: %v", findings)
			}
		})
	}
}

func TestActupFlowNullWhitespace(t *testing.T) {
	source := repositoryWorkflowSource(t, "actup-autofix")
	workflow := parseRepositoryWorkflow(t, "actup-autofix")
	events := make([]string, 0, len(workflow.On))
	for _, event := range workflow.On {
		events = append(events, event.EventName())
	}
	if !slices.Equal(events, []string{"pull_request", "push"}) {
		t.Fatalf("flow null changed trigger selection: %v", events)
	}
	mutated := strings.Replace(string(source), "pull_request: ,", "pull_request:,", 1)
	if mutated == string(source) {
		t.Fatal("missing flow-null mutation target")
	}
	_, findings := workflowparser.ParseDocument([]byte(mutated))
	if len(findings) != 1 || findings[0].Code != string(workflowparser.CodeFlowNull) || findings[0].Start.Line != 8 || findings[0].Start.Column != 7 {
		t.Fatalf("missing compatibility finding at the original flow key: %v", findings)
	}
}

func TestWikiDynamicMappingsAndAliasUses(t *testing.T) {
	workflow := parseRepositoryWorkflow(t, "shellcheck-wiki")
	steps := workflow.Jobs["actionlint"].Steps
	if len(steps) != 2 {
		t.Fatalf("implicit flow mappings lost a step: %v", steps)
	}
	for i, want := range []string{"actions/checkout@v7", "kjanat/actionlint@v1"} {
		action, ok := steps[i].Exec.(*workflowlang.ExecAction)
		if !ok || action.Uses == nil || action.Uses.Value != want {
			t.Fatalf("flow step %d lost uses %q: %+v", i, want, steps[i])
		}
	}
	publish := workflow.Jobs["publish"]
	if publish.Env == nil || len(publish.Env.Insertions) != 1 || publish.Env.Insertions[0].Expression == nil || publish.Env.Insertions[0].Expression.Value != "${{ fromJSON(vars.GIT) }}" {
		t.Fatalf("dynamic environment insertion lost: %+v", publish.Env)
	}
	for i, want := range []string{"${{ fromJSON(vars.REPO) }}", "${{ fromJSON(vars.WIKI) }}"} {
		action, ok := publish.Steps[i].Exec.(*workflowlang.ExecAction)
		if !ok || action.Uses == nil || action.Uses.Value != "actions/checkout@v7" || action.InputsExpression == nil || action.InputsExpression.Value != want {
			t.Fatalf("checkout %d lost its dynamic input mapping: %+v", i, publish.Steps[i])
		}
	}
	if publish.Defaults == nil || publish.Defaults.Run == nil || publish.Defaults.Run.WorkingDirectory == nil || publish.Defaults.Run.WorkingDirectory.Value != "${{ fromJSON(vars.WIKI).path }}" {
		t.Fatalf("dynamic working directory lost: %+v", publish.Defaults)
	}
}

func TestRunnyMatrixSequenceExpansion(t *testing.T) {
	workflow := parseRepositoryWorkflow(t, "runny-matrix-insertion")
	for _, tc := range []struct {
		job   string
		count int
	}{
		{"expression", 4}, {"literal", 3}, {"empty", 2}, {"includes", 4}, {"axes", 2},
	} {
		t.Run(tc.job, func(t *testing.T) {
			combinations, known := matrix.Resolve(workflow.Jobs[tc.job].Strategy).Combinations(16)
			if !known || len(combinations) != tc.count {
				t.Fatalf("want %d matrix configurations, got known=%v %v", tc.count, known, combinations)
			}
		})
	}
}

func TestRunnyConditionExecution(t *testing.T) {
	workflow := parseRepositoryWorkflow(t, "runny-conditions")
	steps := workflow.Jobs["empty-conditions"].Steps
	want := []bool{true, true, true, true, true, true, false, false, false, true, false}
	if len(steps) != len(want) {
		t.Fatalf("want %d condition cases, got %d", len(want), len(steps))
	}
	for i, step := range steps {
		if step.If == nil {
			t.Fatalf("condition missing from step %d", i)
		}
		enabled, known := execution.StepCondition(value.Condition(step.If.Value, value.Origin{}))
		if !known || enabled != want[i] {
			t.Errorf("step %d (%s): enabled=%v known=%v, want %v", i, step.If.Value, enabled, known, want[i])
		}
	}
}

func TestActionsRsNestedExpressionDiagnostics(t *testing.T) {
	source := repositoryWorkflowSource(t, "actions-rs")
	findings := testutil.WorkflowDiagnostics(t, string(source), "repository: {runners: []}\ntools: {shellcheck: false, ruff: false}")
	// The two constant job conditions are truthy. The inner fromJSON argument
	// deliberately contains non-JSON syntax; parsing this YAML must not conceal it.
	truthy, invalidJSON, scriptValues := 0, 0, 0
	for _, finding := range findings {
		switch {
		case finding.Rule == "if-cond" && strings.Contains(finding.Message, "always truthy"):
			truthy++
		case finding.Rule == "expression-call" && finding.Code == "invalid-json":
			invalidJSON++
		case finding.Rule == "script-interpolation" && finding.Code == "unknown-script-value" &&
			(finding.Start.Line == 65 || finding.Start.Line == 76) && strings.Contains(finding.Snippet, "dependencies.actions-rs.path"):
			// A workspace path is unconstrained text inserted into a TOML string.
			// An apostrophe in that path can change the embedded manifest syntax.
			scriptValues++
		default:
			t.Errorf("unexpected finding: %v", finding)
		}
	}
	if truthy != 2 || invalidJSON != 1 || scriptValues != 2 {
		t.Fatalf("want two truthy conditions, one invalid JSON argument and two script substitutions, got %v", findings)
	}
	workflow := parseRepositoryWorkflow(t, "actions-rs")
	roundTrip := workflow.Jobs["round-trip"]
	for _, step := range roundTrip.Steps[1:] {
		script, ok := step.Exec.(*workflowlang.ExecRun)
		if !ok || script.Run == nil || !strings.HasPrefix(script.Run.Value, "---\npackage.edition") || strings.Count(script.Run.Value, "\n---\n") != 1 {
			t.Fatalf("Cargo frontmatter escaped the run scalar: %+v", step)
		}
	}
}

func TestRepositoryReleaseMatrixKnowledge(t *testing.T) {
	for _, tc := range []struct{ workflow, job string }{
		{"runner-release", "upload-assets"}, {"svg-release", "build-and-upload"},
	} {
		t.Run(tc.workflow, func(t *testing.T) {
			workflow := parseRepositoryWorkflow(t, tc.workflow)
			job := workflow.Jobs[tc.job]
			if job == nil || len(job.Needs) != 2 || job.Needs[0].Value != "matrix" || job.Needs[1].Value != "create-release" {
				t.Fatalf("matrix producer dependency lost: %+v", job)
			}
			if combinations, known := matrix.Resolve(job.Strategy).Combinations(256); known {
				t.Fatalf("runtime job output became a closed matrix: %v", combinations)
			}
			if job.ContinueOnError == nil || job.ContinueOnError.Expression == nil || job.ContinueOnError.Expression.Value != "${{ matrix.experimental }}" {
				t.Fatalf("matrix-dependent failure policy lost: %+v", job.ContinueOnError)
			}
			call := workflow.Jobs["call-npm-release"].WorkflowCall
			if call == nil || call.Uses == nil || call.Uses.Value != "$/.github/workflows/npm-release.yml" {
				t.Fatalf("same-repository reusable workflow lost: %+v", call)
			}
		})
	}
}
