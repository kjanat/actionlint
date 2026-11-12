package composites

import (
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/files"
	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
)

func workflowSnapshot(t *testing.T, content []byte, document parsing.Document) parsing.SourceSnapshot {
	t.Helper()
	_, snapshot := registeredWorkflowSnapshot(t, content, document)
	return snapshot
}

// registeredWorkflowSnapshot also returns the registry that admitted the snapshot.
func registeredWorkflowSnapshot(t *testing.T, content []byte, document parsing.Document) (*parsing.SourceSnapshots, parsing.SourceSnapshot) {
	t.Helper()
	registry, _, snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document("fixture.yml", content, document)
	if err != nil {
		t.Fatal(err)
	}
	return registry, snapshot
}

func startWorkflowEntry(t *testing.T, analysis *compositeAnalyzer, registry *parsing.SourceSnapshots, snapshot parsing.SourceSnapshot) {
	t.Helper()
	analysis.sourceSnapshots = registry
	analysis.budget = invocation.DefaultBudget()
	if err := analysis.startEntry(invocation.EntryWorkflow, snapshot); err != nil {
		t.Fatal(err)
	}
	if analysis.scriptScope != nil {
		analysis.scriptScope.Context = analysis.context
	}
}

func TestCompositeOccurrencesShareDeclarationsAndKeepDeploymentState(t *testing.T) {
	t.Parallel()
	source := []byte("name: source\ndescription: identity\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ok\n")
	metadata, err := actionmeta.Parse(filepath.Join(sourceFixtureBase(t), "source", "action.yml"), source)
	if err != nil {
		t.Fatal(err)
	}
	for _, mappedFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "unmapped first", true: "mapped first"}[mappedFirst], func(t *testing.T) {
			t.Parallel()
			plain, mapped := *metadata, *metadata
			mapped.SourceMapped = true
			first, second := &plain, &mapped
			if mappedFirst {
				first, second = second, first
			}
			analysis := &compositeAnalyzer{compositionSession: &compositionSession{ctx: t.Context(), collectScripts: true}, actions: workspaceactions.NewResolver(nil, nil)}
			content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: $/source\n      - uses: $/source\n")
			document := parsing.Workflow(content)
			if len(document.SyntaxErrors) != 0 {
				t.Fatal(document.SyntaxErrors)
			}
			registry, _, snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
			rootSnapshot, err := snapshots.Document("fixture.yml", content, document)
			if err != nil {
				t.Fatal(err)
			}
			workflow := workflowView(t, rootSnapshot)
			calls := workflowStepViews(t, workflow, "test")
			startWorkflowEntry(t, analysis, registry, rootSnapshot)
			if err := analysis.beginWorkflow(workflow); err != nil {
				t.Fatal(err)
			}
			for i, target := range []*actionmeta.Metadata{first, second} {
				view := calls[i]
				facts, err := execution.CallFacts(view, analysis.actions.Platform())
				if err != nil {
					t.Fatal(err)
				}
				site := stepReference(t, analysis.sourceSnapshot, calls[i])
				ticket, err := analysis.invocations.Begin(analysis.invocations.RootContext(), site, facts)
				if err != nil {
					t.Fatal(err)
				}
				context, admitted := ticket.Context()
				if !admitted {
					t.Fatal("fixture call was not admitted")
				}
				snapshot, err := snapshots.Action(files.PhysicalPath(target.Path()), target)
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
				if err := analysis.visitMetadataScripts(target, snapshot, target.Path(), context, nil); err != nil {
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
			snapshot, err := snapshots.Action(files.PhysicalPath(metadata.Path()), metadata)
			if err != nil {
				t.Fatal(err)
			}
			steps := actionStepViews(t, snapshot)
			if len(steps) != 1 {
				t.Fatalf("indexed declarations = %+v", steps)
			}
			run, ok := steps[0].Run()
			if !ok {
				t.Fatal("declaration is not a run")
			}
			for i, target := range []*actionmeta.Metadata{first, second} {
				unit := units[i]
				if !unit.Run.Present() || unit.Run.Run().Value() != run.Run().Value() || unit.Origin.StepPosition.Line != stepPosition(t, steps[0]).Line {
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
			if units[0].Scope == units[1].Scope {
				t.Fatal("distinct occurrences shared mutable execution scope")
			}
			if units[0].Occurrence == units[1].Occurrence {
				t.Fatal("distinct invocations shared script occurrence identity")
			}
		})
	}
}
