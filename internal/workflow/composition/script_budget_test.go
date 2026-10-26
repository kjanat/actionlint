package composition

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	"actionlint.kjanat.dev/v2/source"
)

func TestSelectorFailureWithoutCollectionPreservesRegisteredFacts(t *testing.T) {
	f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo first}, {uses: actions/github-script@v7, with: {script: console.log(1)}}, {run: echo later}]}}\n", nil, nil)
	f.service.options.CollectScripts = false
	failure := errors.New("selector configuration failed")
	f.service.options.ScriptInputsForConfig = func(*repoconfig.Config) ([]analyzer.ScriptInput, error) {
		return nil, failure
	}
	result, err := f.service.Workflow(f.root)
	graph := result.Batch.Invocations
	interruption, stopped := graph.EntryInterruption()
	if !errors.Is(err, failure) || !stopped || interruption.Reason() != invocation.ReasonOperational || len(graph.Scripts()) != 1 || len(result.Batch.Scripts) != 0 {
		t.Fatalf("selector failure without collection: %v, graph=%+v", err, graph)
	}
}

func TestParallelScriptsRegisterWithoutCollection(t *testing.T) {
	workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - parallel:\n          - run: echo child\n          - uses: actions/github-script@v7\n            with: {script: console.log(1)}\n"
	f := newInvocationFixture(t, t.Context(), workflow, nil, nil)
	f.service.options.CollectScripts = false
	result, err := f.service.Workflow(f.root)
	if err != nil {
		t.Fatal(err)
	}
	scripts := result.Batch.Invocations.Scripts()
	if len(scripts) != 2 || scripts[0].ID == scripts[1].ID || scripts[0].Declaration == scripts[1].Declaration || scripts[0].Kind != invocation.ScriptRun || scripts[1].Kind != invocation.ScriptActionInput || len(result.Batch.Scripts) != 0 {
		t.Fatalf("parallel declaration registration lost: %+v", scripts)
	}
}

func TestCompositionExactlyAtScriptLimit(t *testing.T) {
	for _, withInput := range []bool{false, true} {
		for _, collect := range []bool{false, true} {
			t.Run(fmt.Sprintf("collect=%t/input=%t", collect, withInput), func(t *testing.T) {
				budget := invocation.DefaultBudget()
				budget.MaxScripts = 3
				lastStep := "{shell: bash, run: echo last}"
				if withInput {
					lastStep = "{uses: actions/github-script@v7, with: {script: console.log(1)}}"
				}
				f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo root}, {uses: $/child}]}}\n", map[string]string{
					"child/action.yml": "name: child\ndescription: limit\nruns: {using: composite, steps: [{shell: bash, run: echo child}, " + lastStep + "]}\n",
				}, &budget)
				f.service.options.CollectScripts = collect
				result, err := f.service.Workflow(f.root)
				graph := result.Batch.Invocations
				_, stopped := graph.Stop()
				wantUnits := 0
				if collect {
					wantUnits = 3
				}
				if err != nil || stopped || graph.Incomplete() != withInput || len(graph.Scripts()) != 3 || len(result.Batch.Scripts) != wantUnits {
					t.Fatalf("exact limit rejected: %v, graph=%+v", err, graph)
				}
				if withInput {
					calls := graph.Calls()
					if len(calls) != 2 || calls[1].Outcome.Reason() != invocation.ReasonNonLocal {
						t.Fatal("action-input graph must be incomplete only because the remote body is unavailable")
					}
				}
			})
		}
	}
}

func TestNestedBudgetStopRetainsConcurrentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	budget := invocation.DefaultBudget()
	budget.MaxScripts = 1
	f := newInvocationFixture(t, ctx, "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/outer}]}}\n", map[string]string{
		"outer/action.yml": "name: outer\ndescription: cancellation\nruns: {using: composite, steps: [{uses: $/inner}]}\n",
		"inner/action.yml": "name: inner\ndescription: cancellation\nruns: {using: composite, steps: [{shell: bash, run: echo retained}, {uses: actions/github-script@v7, with: {script: console.log(1)}}]}\n",
	}, &budget)
	f.service.options.ScriptInputsForConfig = func(*repoconfig.Config) ([]analyzer.ScriptInput, error) {
		cancel()
		return nil, nil
	}
	result, err := f.service.Workflow(f.root)
	if !errors.Is(err, invocation.ErrScriptBudget) || !errors.Is(err, context.Canceled) || errors.Is(err, invocation.ErrUnfinished) {
		t.Fatalf("nested unwind lost a simultaneous failure: %v", err)
	}
	graph := result.Batch.Invocations
	stop, stopped := graph.Stop()
	calls := graph.Calls()
	if !stopped || stop.Limit != invocation.LimitScripts || len(calls) != 2 || calls[0].Outcome.Kind() != invocation.OutcomeExpanded || calls[1].Outcome.Reason() != invocation.ReasonCanceled || len(graph.Scripts()) != 1 || len(result.Batch.Scripts) != 1 {
		t.Fatalf("nested unwind lost budget, cancellation or retained script: %+v", graph)
	}
}

func TestCallVisitPreservesExistingScriptStop(t *testing.T) {
	budget := invocation.DefaultBudget()
	budget.MaxScripts = 1
	f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo first}, {run: echo second}, {uses: $/later}]}}\n", nil, &budget)
	result, err := f.service.analyze(func(c *compositeAnalyzer) error {
		if err := c.startEntry(invocation.EntryWorkflow, f.root); err != nil {
			return err
		}
		workflow, _ := f.root.Workflow()
		steps := workflow.Jobs["test"].Steps
		for i := range 2 {
			ref, err := f.root.StepReference(steps[i])
			if err != nil {
				return err
			}
			_, err = c.invocations.AddScript(c.occurrence, ref, invocation.ScriptRun, "")
			if i == 0 && err != nil || i == 1 && !errors.Is(err, invocation.ErrScriptBudget) {
				t.Fatalf("setup script %d: %v", i, err)
			}
		}
		return c.visitActionScripts(steps[2], nil, func() error {
			t.Fatal("validated call after a script stop")
			return nil
		})
	})
	if !errors.Is(err, invocation.ErrScriptBudget) || errors.Is(err, invocation.ErrCallBudget) {
		t.Fatalf("call visit replaced stop cause: %v", err)
	}
	if _, interrupted := result.Batch.Invocations.EntryInterruption(); interrupted {
		t.Fatal("expected stop became an operational interruption")
	}
}

func TestBudgetDoesNotHideJoinedFailures(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancelled=%t", canceled), func(t *testing.T) {
			budget := invocation.DefaultBudget()
			budget.MaxScripts = 1
			f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/child}]}}\n", nil, &budget)
			var scriptID, owner invocation.ID
			result, err := f.service.analyze(func(c *compositeAnalyzer) error {
				if err := c.startEntry(invocation.EntryWorkflow, f.root); err != nil {
					return err
				}
				ticket, err := c.invocations.Begin(c.occurrence, f.root.Reference(), "$/child", invocation.ConditionEnabled)
				if err != nil {
					return err
				}
				owner = ticket.ID()
				ref := source.Reference{Document: "child", Revision: "revision", Node: "root"}
				expanded, err := invocation.Expanded(ref)
				if err != nil {
					return err
				}
				if _, err := c.invocations.Resolve(owner, expanded); err != nil {
					return err
				}
				ref.Node = "first"
				scriptID, err = c.invocations.AddScript(owner, ref, invocation.ScriptRun, "")
				if err != nil {
					return err
				}
				ref.Node = "second"
				_, stopErr := c.invocations.AddScript(owner, ref, invocation.ScriptRun, "")
				other := c.invocations.Close(c.invocations.RootID())
				if canceled {
					other = context.Canceled
				}
				if other == nil || !errors.Is(stopErr, invocation.ErrScriptBudget) {
					t.Fatal("missing combined failures")
				}
				return c.interrupt(errors.Join(stopErr, other))
			})
			if !errors.Is(err, invocation.ErrScriptBudget) || errors.Is(err, invocation.ErrUnfinished) || errors.Is(err, context.Canceled) != canceled {
				t.Fatalf("joined failure lost or graph unpublished: %v", err)
			}
			graph := result.Batch.Invocations
			script, exists := graph.Script(scriptID)
			call, found := graph.Call(owner)
			if !exists || !found || script.Owner != owner || call.Outcome.Kind() != invocation.OutcomeInterrupted {
				t.Fatal("joined failure lost registered script or its interrupted owner")
			}
			want := invocation.ReasonOperational
			if canceled {
				want = invocation.ReasonCanceled
			}
			if call.Outcome.Reason() != want {
				t.Fatalf("joined failure reason: %s, want %s", call.Outcome.Reason(), want)
			}
		})
	}
}

func TestScriptBudgetStopsEntryAndRetainsEarlierFacts(t *testing.T) {
	for _, mode := range []string{"workflow", "nested", "standalone", "action-input", "nested-input"} {
		for _, collect := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/collect=%t", mode, collect), func(t *testing.T) {
				budget := invocation.DefaultBudget()
				budget.MaxScripts = 1
				steps := "[{run: echo retained}, {run: echo rejected}, {uses: $/later}]"
				if mode == "action-input" {
					steps = "[{run: echo retained}, {uses: actions/github-script@v7, with: {script: console.log(1)}}, {uses: $/later}]"
				}
				workflow := "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: " + steps + "}, later: {runs-on: ubuntu-latest, steps: [{uses: $/later}]}}\n"
				if mode == "nested" || mode == "nested-input" {
					workflow = "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/outer}, {uses: $/later}]}}\n"
				}
				innerSteps := "[{shell: bash, run: echo retained}, {shell: bash, run: echo rejected}, {uses: $/later}]"
				if mode == "nested-input" {
					innerSteps = "[{shell: bash, run: echo retained}, {uses: actions/github-script@v7, with: {script: console.log(1)}}, {uses: $/later}]"
				}
				f := newInvocationFixture(t, t.Context(), workflow, map[string]string{
					"outer/action.yml": "name: outer\ndescription: budget\nruns: {using: composite, steps: [{uses: $/inner}]}\n",
					"inner/action.yml": "name: inner\ndescription: budget\nruns: {using: composite, steps: " + innerSteps + "}\n",
					"later/action.yml": "name: later\ndescription: unreachable\nruns: {using: composite, steps: []}\n",
				}, &budget)
				f.service.options.CollectScripts = collect
				var result Result
				var err error
				wantCalls, wantDocuments := 0, 0
				if mode == "standalone" {
					metadata, _ := f.cache.LookupMetadata("$/inner")
					result, err = f.service.Action(metadata.Metadata())
					wantDocuments = 1
				} else {
					result, err = f.service.Workflow(f.root)
					if mode == "nested" || mode == "nested-input" {
						wantCalls, wantDocuments = 2, 2
					}
				}
				if !errors.Is(err, invocation.ErrScriptBudget) || errors.Is(err, invocation.ErrUnfinished) {
					t.Fatalf("budget failed to unwind traversal: %v", err)
				}
				graph := result.Batch.Invocations
				stop, exists := graph.Stop()
				if !exists || stop.Limit != invocation.LimitScripts || stop.Max != 1 || !graph.Incomplete() || !graph.Has(stop.Parent) {
					t.Fatalf("missing script boundary: %+v", stop)
				}
				if len(graph.Calls()) != wantCalls || len(f.parsed) != wantDocuments {
					t.Fatalf("work continued after exhaustion: calls=%d documents=%v", len(graph.Calls()), f.parsed)
				}
				wantUnits := 0
				if collect {
					wantUnits = 1
				}
				if len(result.Batch.Scripts) != wantUnits || len(graph.Scripts()) != 1 || (collect && result.Batch.Scripts[0].Run.Run.Value != "echo retained") {
					t.Fatal("earlier script lost or rejected script retained")
				}
				for _, call := range graph.Calls() {
					if call.Outcome.Kind() != invocation.OutcomeExpanded {
						t.Fatalf("budget became an operational failure: %+v", call)
					}
				}
				if _, interrupted := graph.EntryInterruption(); interrupted {
					t.Fatal("budget became an entry interruption")
				}
			})
		}
	}
}
