package composites

import (
	"context"
	"errors"
	"testing"

	declarationchecks "actionlint.kjanat.dev/v2/internal/checking/executor/declarations"
	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/checking/rules/conventions"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"

	scriptexecution "actionlint.kjanat.dev/v2/internal/checking/scripts"
	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

type phaseScriptAnalyzer struct {
	t       *testing.T
	visited *bool
	calls   int
	failure error
}

type cancelBeforeStep struct {
	workflowvisit.Base
	cancel context.CancelFunc
}

func (r *cancelBeforeStep) VisitWorkflowPre(workflowdecl.WorkflowView) error {
	r.cancel()
	return nil
}

func TestCancellationStopsPreparedMetadataCheckBeforeItsStep(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f := newInvocationFixture(t, ctx, "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: actions/checkout@v4}]}}\n", nil, nil)
	check := conventions.MissingActionMetadataCheck(nil)
	rules, err := repoconfig.NewRuleSet([]rulemeta.Declaration{check.Declaration})
	if err != nil {
		t.Fatal(err)
	}
	metadata := declarationchecks.New(rules, check)
	f.options.Rules = []workflowvisit.Rule{&cancelBeforeStep{Base: workflowvisit.NewBase("cancel", ""), cancel: cancel}, metadata}
	_, err = Workflow(f.options, f.root)
	if !errors.Is(err, context.Canceled) || len(metadata.Errs()) != 0 {
		t.Fatalf("canceled execution reached the metadata callback: error=%v findings=%+v", err, metadata.Errs())
	}
}

func TestInvalidPreparationSkipsScriptsButJoinsAndCollectsFindings(t *testing.T) {
	t.Parallel()
	for _, unfinished := range []bool{false, true} {
		t.Run(map[bool]string{false: "invalid batch", true: "unfinished graph"}[unfinished], func(t *testing.T) {
			t.Parallel()
			f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n", nil, nil)
			visited, joined := true, false
			pass := &phaseRule{Base: workflowvisit.NewBase("phase", ""), visited: &visited, joined: &joined}
			script := &phaseScriptAnalyzer{t: t, visited: &visited}
			joinFailure := errors.New("join failure")
			f.options.Rules = []workflowvisit.Rule{pass}
			f.options.Scripts = scriptexecution.Service{Analyzers: []scriptexecution.Analyzer{script}}
			f.options.Join = func() error {
				joined = true
				pass.Error(&workflowlang.Pos{Line: 1, Col: 1}, "reported finding")
				return joinFailure
			}
			result, err := analyze(f.options, func(c *compositeAnalyzer) error {
				if err := c.startEntry(invocation.EntryWorkflow, f.root); err != nil {
					return err
				}
				workflow, _ := f.root.WorkflowView()
				ref := stepReference(t, f.root, workflowStepViews(t, workflow, "test")[0])
				if unfinished {
					_, err := c.invocations.Begin(c.invocations.RootContext(), ref, invocation.Facts{Uses: "$/child", HasUses: true, Condition: invocation.ConditionEnabled})
					return err
				}
				_, err := c.invocations.AddScript(c.invocations.RootContext(), ref, invocation.ScriptRun, "")
				return err
			})
			want := scriptexecution.ErrInvalidBatch
			if unfinished {
				want = invocation.ErrUnfinished
			}
			if !errors.Is(err, want) || !errors.Is(err, joinFailure) || !joined || script.calls != 0 || result.Batch.Valid() || len(result.Findings) != 1 || result.Findings[0].Message != "reported finding" {
				t.Fatalf("invalid preparation escaped: error=%v joined=%v calls=%d result=%+v", err, joined, script.calls, result)
			}
			if !unfinished && (!result.Invocations.Has(result.Invocations.RootID()) || result.Invocations.Entry().Reference != f.root.Reference()) {
				t.Fatal("batch preparation failure discarded the published entry graph")
			}
		})
	}
}

func TestAnalyzerProjectionRequiresPreparedWorkflow(t *testing.T) {
	t.Parallel()
	for _, content := range []string{"on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n", "jobs: ["} {
		parsed := parsing.Workflow([]byte(content))
		input, err := CheckInput(Result{}, "ci.yml", "ci.yml", []byte(content), parsed, nil)
		if parsed.Facts != nil {
			if !errors.Is(err, scriptexecution.ErrInvalidBatch) || len(input.AvailableInputs) != 1 || input.Content != content {
				t.Fatalf("unprepared workflow projected: %v", err)
			}
		} else if err != nil || len(input.AvailableInputs) != 1 || input.Content != content {
			t.Fatalf("failed parse lost source-only analysis: %+v %v", input, err)
		}
	}
}

func TestPreparedBatchRetainsNonCompositeActionEntries(t *testing.T) {
	t.Parallel()
	for _, using := range []string{"node24", "docker"} {
		t.Run(using, func(t *testing.T) {
			t.Parallel()
			body := "main: index.js"
			if using == "docker" {
				body = "image: Dockerfile"
			}
			meta, err := actionmeta.Parse("action.yml", []byte("name: example\ndescription: example\nruns:\n  using: "+using+"\n  "+body+"\n"))
			if err != nil {
				t.Fatal(err)
			}
			result, err := standaloneActionForTest(t, Options{Context: t.Context(), Actions: workspaceactions.NewResolver(nil, nil), CollectScripts: true}, meta)
			if err != nil || !result.Batch.Valid() || len(result.Batch.Scopes()) != 1 || len(result.Batch.Scripts()) != 0 {
				t.Fatalf("non-composite entry rejected: %+v %v", result, err)
			}
		})
	}
}

func (a *phaseScriptAnalyzer) AnalyzeScripts(_ context.Context, batch scriptexecution.Batch) ([]*findingmodel.Finding, error) {
	a.t.Helper()
	if !*a.visited || len(batch.Scripts()) != 1 {
		a.t.Fatalf("scripts ran before traversal: visited=%v scripts=%d", *a.visited, len(batch.Scripts()))
	}
	a.calls++
	return nil, a.failure
}

type phaseRule struct {
	workflowvisit.Base
	visited *bool
	failure error
	joined  *bool
}

func (r *phaseRule) VisitStep(workflowdecl.StepView) error { *r.visited = true; return r.failure }
func TestCompositionCompletesScriptsAndResourcesAfterVisitFailure(t *testing.T) {
	t.Parallel()
	for _, failVisit := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "visit failure"}[failVisit], func(t *testing.T) {
			t.Parallel()
			parsed := parsing.Workflow([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"))
			scriptFailure := errors.New("script failure")
			visitFailure := errors.New("visit failure")
			visited, joined := false, false
			pass := &phaseRule{Base: workflowvisit.NewBase("phase", ""), visited: &visited, joined: &joined}
			if failVisit {
				pass.failure = visitFailure
			}
			script := &phaseScriptAnalyzer{t: t, visited: &visited, failure: scriptFailure}
			options := Options{
				Context: t.Context(), Actions: workspaceactions.NewResolver(nil, nil), Rules: []workflowvisit.Rule{pass},
				CollectScripts: true, Scripts: scriptexecution.Service{Analyzers: []scriptexecution.Analyzer{script}}, Join: func() error {
					joined = true
					pass.Error(&workflowlang.Pos{Line: 1, Col: 1}, "joined finding")
					return nil
				},
			}
			registry, _, snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
			snapshot, err := snapshots.Document("workflow.yml", []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"), parsed)
			if err != nil {
				t.Fatal(err)
			}
			options.SourceSnapshots = registry
			result, err := Workflow(options, snapshot)
			if !errors.Is(err, scriptFailure) || errors.Is(err, visitFailure) != failVisit || !joined || script.calls != 1 || len(result.Batch.Scripts()) != 1 || len(result.Findings) != 1 || result.Findings[0].Message != "joined finding" {
				t.Fatalf("error=%v joined=%v calls=%d result=%+v", err, joined, script.calls, result)
			}
		})
	}
}
