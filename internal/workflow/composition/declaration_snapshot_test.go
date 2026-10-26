package composition

import (
	"path/filepath"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/fsread"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
)

func workflowSnapshot(t *testing.T, workflow *workflowlang.Workflow) *documentparse.SourceSnapshot {
	t.Helper()
	var snapshots documentparse.SourceSnapshots
	snapshot, err := snapshots.Document("fixture.yml", nil, documentparse.Document{Workflow: workflow})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func startWorkflowEntry(t *testing.T, analysis *compositeAnalyzer, snapshot *documentparse.SourceSnapshot) {
	t.Helper()
	analysis.budget = invocation.DefaultBudget()
	if err := analysis.startEntry(invocation.EntryWorkflow, snapshot); err != nil {
		t.Fatal(err)
	}
	if analysis.scriptScope != nil {
		analysis.scriptScope.Context = analysis.context
	}
}

func TestCompositeOccurrencesShareDeclarationsAndKeepDeploymentState(t *testing.T) {
	source := []byte("name: source\ndescription: identity\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ok\n")
	metadata, err := actionmeta.Parse("source/action.yml", source)
	if err != nil {
		t.Fatal(err)
	}
	for _, mappedFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "unmapped first", true: "mapped first"}[mappedFirst], func(t *testing.T) {
			plain, mapped := *metadata, *metadata
			mapped.SourceMapped = true
			first, second := &plain, &mapped
			if mappedFirst {
				first, second = second, first
			}
			analysis := &compositeAnalyzer{ctx: t.Context(), collectScripts: true, actions: actionlocal.NewResolver(nil, nil)}
			calls := []*workflowlang.Step{
				{Pos: &workflowlang.Pos{Line: 1, Col: 1}, Exec: &workflowlang.ExecAction{Uses: &workflowlang.String{Value: "$/source"}}},
				{Pos: &workflowlang.Pos{Line: 2, Col: 1}, Exec: &workflowlang.ExecAction{Uses: &workflowlang.String{Value: "$/source"}}},
			}
			workflow := &workflowlang.Workflow{Jobs: map[string]*workflowlang.Job{"": {Steps: calls}}}
			startWorkflowEntry(t, analysis, workflowSnapshot(t, workflow))
			analysis.beginWorkflow(workflow)
			for i, target := range []*actionmeta.Metadata{first, second} {
				facts, err := execution.CallFacts(calls[i], analysis.actions.Platform())
				if err != nil {
					t.Fatal(err)
				}
				site, err := analysis.sourceSnapshot.StepReference(calls[i])
				if err != nil {
					t.Fatal(err)
				}
				ticket, err := analysis.invocations.Begin(analysis.invocations.RootContext(), site, facts)
				if err != nil {
					t.Fatal(err)
				}
				context, admitted := ticket.Context()
				if !admitted { t.Fatal("fixture call was not admitted") }
				if analysis.sourceSnapshots == nil {
					analysis.sourceSnapshots = &documentparse.SourceSnapshots{}
				}
				snapshot, err := analysis.sourceSnapshots.Action(fsread.PhysicalPath(target.Path()), target)
				if err != nil {
					t.Fatal(err)
				}
				outcome, err := invocation.Expanded(snapshot.Reference())
				if err != nil {
					t.Fatal(err)
				}
				if _, err := analysis.invocations.Resolve(context, outcome); err != nil {
					t.Fatal(err)
				}
				analysis.context = context
				if err := analysis.visitMetadataScripts(target, context, nil); err != nil {
					t.Fatal(err)
				}
				if err := analysis.invocations.Close(context); err != nil {
					t.Fatal(err)
				}
				analysis.context = analysis.invocations.RootContext()
			}
			if len(analysis.scripts) != 2 {
				t.Fatalf("script occurrences = %d, want 2", len(analysis.scripts))
			}
			snapshot, err := analysis.sourceSnapshots.Action(fsread.PhysicalPath(metadata.Path()), metadata)
			if err != nil {
				t.Fatal(err)
			}
			steps, ok := snapshot.ActionSteps()
			if !ok || len(steps) != 1 {
				t.Fatalf("indexed declarations = %+v", steps)
			}
			switch run := steps[0].Exec.(type) {
			case *workflowlang.ExecRun:
				for i, target := range []*actionmeta.Metadata{first, second} {
					unit := analysis.scripts[i]
					if unit.Run != run || unit.Origin.StepPosition != steps[0].Pos {
						t.Fatal("script occurrence was decoded outside its declaration snapshot")
					}
					want := filepath.Dir(metadata.Path())
					if target.SourceMapped {
						want = ""
					}
					if unit.Scope.ActionPath != want || unit.Scope.Paths.ActionPath != want {
						t.Fatalf("occurrence %d inherited deployment state: %+v, want action path %q", i, unit.Scope, want)
					}
				}
			default:
				t.Fatalf("unexpected declaration %T", steps[0].Exec)
			}
			if analysis.scripts[0].Scope == analysis.scripts[1].Scope {
				t.Fatal("distinct occurrences shared mutable execution scope")
			}
			if analysis.scripts[0].Occurrence == analysis.scripts[1].Occurrence {
				t.Fatal("distinct invocations shared script occurrence identity")
			}
		})
	}
}
