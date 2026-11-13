package workflow_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	workflowconditions "actionlint.kjanat.dev/v2/internal/model/workflows/conditions"
	"actionlint.kjanat.dev/v2/internal/model/workflows/matrix"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

func repositoryWorkflowSource(t *testing.T, name string) []byte {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("internal", "development", "tests", "integration", "workflow", "testdata", "repositories", name+".yml"))
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
	workflow, findings := workflowparser.Parse(repositoryWorkflowSource(t, name))
	if len(findings) != 0 || workflow == nil {
		t.Fatalf("repository workflow rejected: %v", findings)
	}
	return workflow
}

func TestRepositoryWorkflowSyntax(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"actions-rs", "actup-autofix", "shellcheck-wiki", "runny-matrix-insertion",
		"runny-conditions", "runner-release", "svg-release", "vw-release",
		"kp2bw-windows-bw-cmd",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			parseRepositoryWorkflow(t, name)
		})
	}
}

func TestRepositoryWorkflowAnalysis(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"actup-autofix", "shellcheck-wiki", "runny-matrix-insertion",
		"runner-release", "svg-release", "vw-release",
		"kp2bw-windows-bw-cmd",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			findings := testutil.WorkflowDiagnostics(t, string(repositoryWorkflowSource(t, name)), "repository: {runners: []}\ntools: {shellcheck: false, ruff: false}")
			if name == "kp2bw-windows-bw-cmd" {
				if len(findings) != 1 || findings[0].Severity != "info" || findings[0].Rule != "expression-call" || findings[0].Code != "permissive-json" || findings[0].Message != "you naughty boy, I see you" || findings[0].Start.Line != 24 {
					t.Fatalf("expected only the loose JSON information: %v", findings)
				}
				return
			}
			if len(findings) != 0 {
				t.Fatalf("unexpected repository workflow findings: %v", findings)
			}
		})
	}
}

func TestKP2BWWindowsMatrix(t *testing.T) {
	t.Parallel()
	workflow := parseRepositoryWorkflow(t, "kp2bw-windows-bw-cmd")
	job := workflow.Jobs["cmd-shim"]
	if job == nil || job.RunsOn == nil || job.Defaults == nil || job.Defaults.Run == nil || job.Defaults.Run.Shell == nil || job.Defaults.Run.Shell.Value != "pwsh" {
		t.Fatal("Windows job or PowerShell defaults were not recognized")
	}
	if len(job.RunsOn.Labels) != 1 || job.RunsOn.Labels[0].Value != "windows-latest" {
		t.Fatalf("Windows runner selection was not recognized: %+v", job.RunsOn)
	}
	combinations, known := matrix.Resolve(job.Strategy).Combinations(16)
	if !known || len(combinations) != 4 {
		t.Fatalf("expected four known matrix combinations, got known=%v rows=%v", known, combinations)
	}
	action, ok := job.Steps[1].Exec.(*workflowlang.ExecAction)
	if !ok || len(action.InputInsertions) != 1 || action.InputInsertions[0].Expression == nil {
		t.Fatalf("mise action's dynamic input insertion was not recognized: %+v", job.Steps[1])
	}
	seen := make(map[string]bool)
	for _, combination := range combinations {
		pm, _ := combination.Get("pm")
		cli, _ := combination.Get("cli")
		packageManager, pmKnown := pm.Text()
		command, cliKnown := cli.Text()
		if !pmKnown || !cliKnown || pm.Kind() != value.String || cli.Kind() != value.String {
			t.Fatalf("matrix axes lost their string type or value: %v", combination)
		}
		seen[packageManager+"/"+command] = true
		tools, hasTools := combination.Get("tools")
		if packageManager == "bun" {
			if tool, known := tools.Text(); !hasTools || !known || tool != "bun" || tools.Kind() != value.String {
				t.Fatalf("JSONC include lost its bun tool: %v", combination)
			}
		} else if hasTools {
			t.Fatalf("bun-only tools leaked into npm row: %v", combination)
		}
		inputs := combination.Evaluate(action.InputInsertions[0].Expression.Value)
		installArgs, present := inputs.Prop("install_args")
		wantArgs := "nodejs uv python "
		if packageManager == "bun" {
			wantArgs += "bun"
		}
		if got, known := installArgs.Text(); !inputs.Known() || inputs.Kind() != value.Object || !present || !known || got != wantArgs {
			t.Fatalf("%s/%s install_args: got %v, want %q", packageManager, command, inputs, wantArgs)
		}
		wantPackage := "@bitwarden/cli"
		if command == "vw" {
			wantPackage = "@kjanat/vaultwarden-cli"
		}
		if job.Env == nil || len(job.Env.Vars) != 2 {
			t.Fatal("job environment declarations were not recognized")
		}
		for _, variable := range job.Env.Vars {
			want := map[string]string{"KP2BW_CLI": command, "CLI_PACKAGE": wantPackage}[variable.Name.Value]
			resolved := combination.Evaluate(variable.Value.Value)
			if got, known := resolved.Text(); !known || got != want {
				t.Fatalf("%s/%s %s: got %v, want %q", packageManager, command, variable.Name.Value, resolved, want)
			}
		}
	}
	for _, key := range []string{"npm/bw", "npm/vw", "bun/bw", "bun/vw"} {
		if !seen[key] {
			t.Errorf("matrix combination %s was not inferred", key)
		}
	}
}

func TestActupFlowNullWhitespace(t *testing.T) {
	t.Parallel()
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
	_, findings := workflowparser.Parse([]byte(mutated))
	if len(findings) != 1 || findings[0].Code != workflowparser.CodeFlowNull || findings[0].Line != 8 || findings[0].Column != 7 {
		t.Fatalf("missing compatibility finding at the original flow key: %v", findings)
	}
}

func TestWikiDynamicMappingsAndAliasUses(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	workflow := parseRepositoryWorkflow(t, "runny-matrix-insertion")
	for _, tc := range []struct {
		job   string
		count int
	}{
		{"expression", 4}, {"literal", 3}, {"empty", 2}, {"includes", 4}, {"axes", 2},
	} {
		t.Run(tc.job, func(t *testing.T) {
			t.Parallel()
			combinations, known := matrix.Resolve(workflow.Jobs[tc.job].Strategy).Combinations(16)
			if !known || len(combinations) != tc.count {
				t.Fatalf("want %d matrix configurations, got known=%v %v", tc.count, known, combinations)
			}
		})
	}
}

func TestRunnyConditionExecution(t *testing.T) {
	t.Parallel()
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
		enabled, known := workflowconditions.StepCondition(value.Condition(step.If.Value, value.Origin{}))
		if !known || enabled != want[i] {
			t.Errorf("step %d (%s): enabled=%v known=%v, want %v", i, step.If.Value, enabled, known, want[i])
		}
	}
}

func TestActionsRsNestedExpressionDiagnostics(t *testing.T) {
	t.Parallel()
	source := repositoryWorkflowSource(t, "actions-rs")
	findings := testutil.WorkflowDiagnostics(t, string(source), "repository: {runners: []}\ntools: {shellcheck: false, ruff: false}")
	// The two constant job conditions are truthy. The inner fromJSON uses valid
	// loose runner syntax, verified against both Runny engines; it is not an error.
	truthy, scriptValues, information := 0, 0, 0
	for _, finding := range findings {
		switch {
		case finding.Rule == "expression-call" && finding.Code == "permissive-json" && finding.Severity == "info" &&
			finding.Message == "you naughty boy, I see you" && finding.Start.Line == 39:
			information++
		case finding.Rule == "if-cond" && strings.Contains(finding.Message, "always truthy"):
			truthy++
		case finding.Rule == "script-interpolation" && finding.Code == "unknown-script-value" &&
			(finding.Start.Line == 65 || finding.Start.Line == 76) && strings.Contains(finding.Snippet, "dependencies.actions-rs.path"):
			// A workspace path is unconstrained text inserted into a TOML string.
			// An apostrophe in that path can change the embedded manifest syntax.
			scriptValues++
		default:
			t.Errorf("unexpected finding: %v", finding)
		}
	}
	if truthy != 2 || scriptValues != 2 || information != 1 {
		t.Fatalf("want two truthy conditions, two script substitutions and one information diagnostic, got %v", findings)
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
	t.Parallel()
	for _, tc := range []struct{ workflow, job string }{
		{"runner-release", "upload-assets"}, {"svg-release", "build-and-upload"},
	} {
		t.Run(tc.workflow, func(t *testing.T) {
			t.Parallel()
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
