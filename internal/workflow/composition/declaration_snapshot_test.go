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

func workflowSnapshot(t *testing.T, content []byte, document documentparse.Document) *documentparse.SourceSnapshot {
	t.Helper()
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document("fixture.yml", content, document)
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
	metadata, err := actionmeta.Parse(filepath.Join(sourceFixtureBase(t), "source", "action.yml"), source)
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
			content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: $/source\n      - uses: $/source\n")
			document := documentparse.Workflow("fixture.yml", content)
			if len(document.Findings) != 0 {
				t.Fatal(document.Findings)
			}
			workflow := document.Workflow
			calls := workflow.Jobs["test"].Steps
			snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
			rootSnapshot, err := snapshots.Document("fixture.yml", content, document)
			if err != nil {
				t.Fatal(err)
			}
			startWorkflowEntry(t, analysis, rootSnapshot)
			if err := analysis.beginWorkflow(workflow); err != nil {
				t.Fatal(err)
			}
			for i, target := range []*actionmeta.Metadata{first, second} {
				view, err := analysis.sourceSnapshot.StepView(calls[i])
				if err != nil {
					t.Fatal(err)
				}
				facts, err := execution.CallFacts(view, analysis.actions.Platform())
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
				if !admitted {
					t.Fatal("fixture call was not admitted")
				}
				snapshot, err := snapshots.Action(fsread.PhysicalPath(target.Path()), target)
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
			batch := prepareCollectedScripts(t, analysis)
			units := batch.Scripts()
			snapshot, err := snapshots.Action(fsread.PhysicalPath(metadata.Path()), metadata)
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
					unit := units[i]
					if !unit.Run.Present() || unit.Run.Run().Value() != run.Run.Value || unit.Origin.StepPosition.Line != steps[0].Pos.Line {
						t.Fatal("script occurrence was decoded outside its declaration snapshot")
					}
					want := filepath.Dir(metadata.Path())
					if target.SourceMapped {
						want = ""
					}
					if unit.Scope.Deployment.ActionPath() != want || unit.Scope.Paths.ActionPath != want {
						t.Fatalf("occurrence %d inherited deployment state: %+v, want action path %q", i, unit.Scope, want)
					}
				}
			default:
				t.Fatalf("unexpected declaration %T", steps[0].Exec)
			}
			if units[0].Scope == units[1].Scope {
				t.Fatal("distinct occurrences shared mutable execution scope")
			}
			if units[0].Occurrence == units[1].Occurrence {
				t.Fatal("distinct invocations shared script occurrence identity")
			}
		})
	}
}
