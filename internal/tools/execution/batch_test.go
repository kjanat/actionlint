package execution

import (
	"errors"
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowexecution "actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
)

func batchFixture(t *testing.T, withInput bool) (invocation.Graph, []*Scope, []Unit) {
	t.Helper()
	content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n")
	if withInput {
		content = append(content, []byte("      - uses: actions/github-script@v7\n        with: {script: console.log(1)}\n")...)
	}
	parsed := documentparse.Workflow("ci.yml", content)
	if len(parsed.Findings) != 0 {
		t.Fatal(parsed.Findings)
	}
	var snapshots documentparse.SourceSnapshots
	snapshot, err := snapshots.Document("ci.yml", content, parsed)
	if err != nil {
		t.Fatal(err)
	}
	builder, err := invocation.NewBuilder(invocation.EntryWorkflow, snapshot.Reference(), invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	scope := &Scope{Context: builder.RootContext()}
	var units []Unit
	for _, step := range parsed.Workflow.Jobs["test"].Steps {
		ref, err := snapshot.StepReference(step)
		if err != nil {
			t.Fatal(err)
		}
		unit := Unit{Scope: scope, Reference: ref, Path: "ci.yml", Content: content, Owner: &findingmodel.Owner{Job: "test", Step: step.Pos}}
		kind, input := invocation.ScriptRun, ""
		switch body := step.Exec.(type) {
		case *workflowlang.ExecRun:
			unit.Run = body
		case *workflowlang.ExecAction:
			kind, input = invocation.ScriptActionInput, "script"
			unit.Input = &ActionInput{Site: analyzer.ScriptInput{Action: "actions/github-script", Input: input, Language: "javascript"}, Value: body.Inputs[input]}
		default:
			t.Fatal("unexpected fixture step")
		}
		unit.Occurrence, err = builder.AddScript(builder.RootContext(), ref, kind, input)
		if err != nil {
			t.Fatal(err)
		}
		units = append(units, unit)
	}
	graph, err := builder.Publish()
	if err != nil {
		t.Fatal(err)
	}
	return graph, []*Scope{scope}, units
}

func TestBatchRejectsForeignOrMalformedInputs(t *testing.T) {
	for name, mutate := range map[string]func(*invocation.Graph, *[]*Scope, *[]Unit){
		"missing run mapping":    func(_ *invocation.Graph, _ *[]*Scope, u *[]Unit) { (*u)[0].Run.Source = nil },
		"missing run position":   func(_ *invocation.Graph, _ *[]*Scope, u *[]Unit) { (*u)[0].Run.Run.Pos = nil },
		"missing run key":        func(_ *invocation.Graph, _ *[]*Scope, u *[]Unit) { (*u)[0].Run.RunPos = nil },
		"missing input mapping":  func(_ *invocation.Graph, _ *[]*Scope, u *[]Unit) { (*u)[1].Input.Value.Source = nil },
		"missing input position": func(_ *invocation.Graph, _ *[]*Scope, u *[]Unit) { (*u)[1].Input.Value.Value.Pos = nil },
		"missing input key":      func(_ *invocation.Graph, _ *[]*Scope, u *[]Unit) { (*u)[1].Input.Value.Name.Pos = nil },
		"unpublished graph":      func(g *invocation.Graph, _ *[]*Scope, _ *[]Unit) { *g = invocation.Graph{} },
		"nil scope":              func(_ *invocation.Graph, s *[]*Scope, _ *[]Unit) { (*s)[0] = nil },
		"unknown scope":          func(_ *invocation.Graph, s *[]*Scope, _ *[]Unit) { (*s)[0].Context = invocation.Context{} },
		"foreign scope pointer":  func(_ *invocation.Graph, _ *[]*Scope, u *[]Unit) { (*u)[0].Scope = copyValue((*u)[0].Scope) },
		"foreign occurrence":     func(_ *invocation.Graph, _ *[]*Scope, u *[]Unit) { (*u)[0].Occurrence = "unknown" },
		"stale source":           func(_ *invocation.Graph, _ *[]*Scope, u *[]Unit) { (*u)[0].Reference.Revision = "stale" },
		"duplicate occurrence":   func(_ *invocation.Graph, _ *[]*Scope, u *[]Unit) { (*u)[1] = (*u)[0] },
		"missing body":           func(_ *invocation.Graph, _ *[]*Scope, u *[]Unit) { (*u)[0].Run = nil },
		"two bodies":             func(_ *invocation.Graph, _ *[]*Scope, u *[]Unit) { (*u)[0].Input = (*u)[1].Input },
		"missing input name":     func(_ *invocation.Graph, _ *[]*Scope, u *[]Unit) { (*u)[1].Input.Value.Name = nil },
		"wrong input":            func(_ *invocation.Graph, _ *[]*Scope, u *[]Unit) { (*u)[1].Input.Site.Input = "other" },
		"foreign config":         func(_ *invocation.Graph, _ *[]*Scope, u *[]Unit) { (*u)[0].Config = &repoconfig.Config{} },
		"missing unit":           func(_ *invocation.Graph, _ *[]*Scope, u *[]Unit) { *u = (*u)[:1] },
	} {
		t.Run(name, func(t *testing.T) {
			graph, scopes, units := batchFixture(t, true)
			mutate(&graph, &scopes, &units)
			batch, err := NewBatch(graph, scopes, units, true)
			if !errors.Is(err, ErrInvalidBatch) || batch.Valid() {
				t.Fatalf("invalid input bound: %v", err)
			}
		})
	}
}

func TestBatchSnapshotsOwnMutableScriptValues(t *testing.T) {
	graph, scopes, units := batchFixture(t, true)
	scopes[0].Paths.Placements = &workflowexecution.CheckoutPlacement{Directory: workflowexecution.Directory{Kind: workflowexecution.DirectoryKnown, Path: "original"}}
	batch, err := NewBatch(graph, scopes, units, true)
	if err != nil {
		t.Fatal(err)
	}
	first, ok := batch.Fact(0, false)
	if !ok {
		t.Fatal("missing bound fact")
	}
	input, ok := batch.Fact(1, false)
	if !ok {
		t.Fatal("missing input fact")
	}
	units[0].Run.Run.Value = "corrupt original"
	units[0].Run.Source.Spans[0].YamlLine = 999
	units[1].Input.Value.Value.Value = "corrupt input"
	units[0].Content[0] = 'X'
	units[0].Owner.Step.Line = 999
	scopes[0].Context = invocation.Context{}
	scopes[0].Paths.Placements.Directory.Path = "corrupt placement"
	clonedScopes, clonedUnits := batch.Snapshot()
	if clonedUnits[0].Scope != clonedScopes[0] || clonedUnits[1].Scope != clonedScopes[0] || &clonedUnits[0].Content[0] != &clonedUnits[1].Content[0] {
		t.Fatal("snapshot lost shared scope/document ownership")
	}
	clonedScopes[0].Context = invocation.Context{}
	clonedScopes[0].Paths.Placements.Directory.Path = "corrupt returned placement"
	clonedUnits[0].Run.Run = nil
	clonedUnits[0].Run.Source.Spans[0].YamlLine = 888
	clonedUnits[1].Input.Value.Value.Value = "corrupt returned input"
	clonedUnits[0].Content[0] = 'Y'
	after, _ := batch.Fact(0, false)
	afterInput, _ := batch.Fact(1, false)
	if !reflect.DeepEqual(first, after) || !reflect.DeepEqual(input, afterInput) {
		t.Fatal("caller mutation changed a prepared fact")
	}
	_, fresh := batch.Snapshot()
	if fresh[0].Scope.Paths.Placements.Directory.Path != "original" {
		t.Fatal("snapshot mutation reached retained checkout placement")
	}
	if fresh[0].Content[0] != 'o' || fresh[0].Owner.Step.Line == 999 || fresh[0].Scope.Context.ID() != graph.RootID() {
		t.Fatal("snapshot mutation reached retained provenance")
	}
}

func TestScopesOnlyBatchRetainsGraphWithoutMaterializedScripts(t *testing.T) {
	graph, scopes, _ := batchFixture(t, false)
	batch, err := NewBatch(graph, scopes, nil, false)
	if err != nil || !batch.Valid() || batch.CollectedScripts() || len(batch.Scripts()) != 0 || len(batch.Invocations().Scripts()) != 1 {
		t.Fatalf("scopes-only batch: %v", err)
	}
	if _, ok := batch.Fact(0, false); ok {
		t.Fatal("fabricated uncollected fact")
	}
}

func TestScopesOnlyBatchCannotDropCompletedScopes(t *testing.T) {
	graph, scopes, _ := batchFixture(t, false)
	if _, err := NewBatch(graph, nil, nil, false); !errors.Is(err, ErrInvalidBatch) {
		t.Fatalf("missing entry accepted: %v", err)
	}
	builder, err := invocation.NewBuilder(invocation.EntryWorkflow, graph.Entry().Reference, invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := builder.Begin(builder.RootContext(), graph.Scripts()[0].Declaration, invocation.Facts{Uses: "$/child", HasUses: true, Condition: invocation.ConditionEnabled})
	if err != nil {
		t.Fatal(err)
	}
	callContext, admitted := ticket.Context()
	if !admitted { t.Fatal("fixture call was not admitted") }
	scopes[0].Context = builder.RootContext()
	target := graph.Entry().Reference
	target.Document = "child"
	outcome, err := invocation.Expanded(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.Resolve(callContext, outcome); err != nil {
		t.Fatal(err)
	}
	if err := builder.Close(callContext); err != nil {
		t.Fatal(err)
	}
	graph, err = builder.Publish()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewBatch(graph, scopes, nil, false); !errors.Is(err, ErrInvalidBatch) {
		t.Fatalf("missing completed scope accepted: %v", err)
	}
}

func TestUnpreparedBatchCannotExecute(t *testing.T) {
	for name, adapter := range map[string]Analyzer{
		"fact": FactAnalyzer{Analyzer: partialFactAnalyzer{t: t}},
		"tool": Tool{Runner: partialTool{t: t}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := adapter.AnalyzeScripts(t.Context(), Batch{}); !errors.Is(err, ErrInvalidBatch) {
				t.Fatalf("adapter accepted unprepared batch: %v", err)
			}
			if _, err := (Service{Analyzers: []Analyzer{adapter}}).Run(t.Context(), Batch{}); !errors.Is(err, ErrInvalidBatch) {
				t.Fatalf("scheduler accepted unprepared batch: %v", err)
			}
		})
	}
}

func TestConcurrentBatchConsumersCannotMutateRetainedFacts(t *testing.T) {
	graph, scopes, units := batchFixture(t, true)
	units[0].Run.Source.Comments = []workflowlang.ScalarComment{{Text: "original", Pos: workflowlang.Pos{Line: 1, Col: 1}}}
	batch, err := NewBatch(graph, scopes, units, true)
	if err != nil {
		t.Fatal(err)
	}
	for range 4 {
		t.Run("consumer", func(t *testing.T) {
			t.Parallel()
			for range 25 {
				fact, ok := batch.Fact(0, false)
				if !ok || fact.Text != "echo ok" || fact.Comments[0].Text != "original" || fact.Spans[0].Document.Line == 999 {
					t.Fatal("shared mutable fact")
				}
				fact.Comments[0].Text = "changed"
				fact.Spans[0].Document.Line = 999
				_, snapshot := batch.Snapshot()
				snapshot[0].Run.Run.Value = "changed"
				snapshot[0].Content[0] = 'X'
			}
		})
	}
}
