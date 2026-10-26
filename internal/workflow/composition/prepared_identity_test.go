package composition

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
)

func TestWorkflowEntryClearsInheritedActionOrigin(t *testing.T) {
	f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo root}]}}\n", nil, nil)
	paths := &f.service.options.Paths
	paths.ActionPath, paths.ActionRunnerPath, paths.ActionIndependent = "stale source", "stale runner", true
	paths.Placements = &execution.CheckoutPlacement{Directory: execution.Directory{Kind: execution.DirectoryKnown, Path: "checkout"}}
	result, err := f.service.Workflow(f.root)
	if err != nil {
		t.Fatal(err)
	}
	scopes := result.Batch.Scopes()
	if len(scopes) != 1 {
		t.Fatal("fixture must have only an entry scope")
	}
	got := scopes[0].Paths
	if got.ActionPath != "" || got.ActionRunnerPath != "" || got.ActionIndependent || got.Workspace != paths.Workspace || got.Analysis != paths.Analysis || got.Placements == nil || got.Placements.Directory.Path != "checkout" {
		t.Fatalf("entry origin or shared paths incorrect: %+v", got)
	}
}

func TestPreparedBatchRejectsSwappedCallContexts(t *testing.T) {
	f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/child}, {uses: $/child}]}}\n", map[string]string{
		"child/action.yml": "name: child\ndescription: child\nruns: {using: composite, steps: [{shell: bash, run: echo child}]}\n",
	}, nil)
	result, err := f.service.Workflow(f.root)
	if err != nil {
		t.Fatal(err)
	}
	scopes, scripts := result.Batch.Snapshot()
	if len(scopes) != 3 || len(scripts) != 2 {
		t.Fatal("fixture did not expand both calls")
	}
	scopes[1].Context, scopes[2].Context = scopes[2].Context, scopes[1].Context
	if _, err := scriptexecution.NewBatch(result.Batch.Invocations(), scopes, scripts, true); !errors.Is(err, scriptexecution.ErrInvalidBatch) {
		t.Fatalf("swapped call contexts accepted: %v", err)
	}
}

func TestPreparedBatchRejectsForeignAndDerivedContexts(t *testing.T) {
	workflow := "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/child}]}}\n"
	actions := map[string]string{"child/action.yml": "name: child\ndescription: child\nruns: {using: composite, steps: [{shell: bash, run: echo child}]}\n"}
	f := newInvocationFixture(t, t.Context(), workflow, actions, nil)
	first, err := f.service.Workflow(f.root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.Workflow(f.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"zero context", "foreign context", "derived path"} {
		t.Run(change, func(t *testing.T) {
			scopes, units := first.Batch.Snapshot()
			want := "foreign invocation context"
			switch change {
			case "zero context":
				scopes[1].Context = invocation.Context{}
			case "foreign context":
				foreign := second.Batch.Scopes()[1].Context
				if foreign.ID() != scopes[1].Context.ID() {
					t.Fatal("fixture IDs differ")
				}
				scopes[1].Context = foreign
			case "derived path":
				scopes[1].Paths.ActionIndependent = !scopes[1].Paths.ActionIndependent
				want = "action paths disagree"
			}
			if _, err := scriptexecution.NewBatch(first.Batch.Invocations(), scopes, units, true); !errors.Is(err, scriptexecution.ErrInvalidBatch) || !strings.Contains(err.Error(), want) {
				t.Fatalf("%s accepted: %v", change, err)
			}
		})
	}
}

func TestPreparedFactsProjectCanonicalCallCoordinates(t *testing.T) {
	line := "{name: é, description: outer, runs: {using: composite, steps: [{uses: $/inner}]}}"
	f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/outer}]}}\n", map[string]string{
		"outer/action.yml": line + "\n",
		"inner/action.yml": "name: inner\ndescription: inner\nruns: {using: composite, steps: [{shell: bash, run: echo inner}]}\n",
	}, nil)
	result, err := f.service.Workflow(f.root)
	if err != nil {
		t.Fatal(err)
	}
	graph := result.Batch.Invocations()
	if len(graph.Scripts()) != 1 {
		t.Fatal("fixture must produce one nested script")
	}
	call, exists := graph.Call(graph.Scripts()[0].Owner)
	if !exists || call.Uses() != "$/inner" || call.Depth() != 2 {
		t.Fatal("nested script lost its owning call")
	}
	projection, exists := result.Batch.Fact(0, false)
	if !exists || projection.Invocation == nil {
		t.Fatal("missing invocation projection")
	}
	prefix, _, found := strings.Cut(line, "{uses")
	if !found {
		t.Fatal("fixture has no call mapping")
	}
	wantColumn := utf8.RuneCountInString(prefix) + 1
	got := projection.Invocation
	if got.Reference != call.Site() || got.Step.Line != 1 || got.Step.Column != wantColumn || got.Depth != 2 || got.Uses != "$/inner" || got.Caller != call.Site().Document || got.Caller == "" {
		t.Fatalf("projection lost canonical call facts: %+v", got)
	}
}

func TestSharedCallDeclarationRetainsPlatformSpecificFacts(t *testing.T) {
	f := newInvocationFixture(t, t.Context(), "on: push\njobs:\n  linux:\n    runs-on: ubuntu-latest\n    steps: [{uses: $/outer}]\n  windows:\n    runs-on: windows-latest\n    steps: [{uses: $/outer}]\n", map[string]string{
		"outer/action.yml": "name: outer\ndescription: outer\nruns: {using: composite, steps: [{uses: $/inner, env: {Path: custom}}]}\n",
		"inner/action.yml": "name: inner\ndescription: inner\nruns: {using: composite, steps: [{shell: bash, run: echo inner}]}\n",
	}, nil)
	result, err := f.service.Workflow(f.root)
	if err != nil {
		t.Fatal(err)
	}
	var inner []invocation.Call
	for _, call := range result.Batch.Invocations().Calls() {
		if call.Uses() == "$/inner" {
			inner = append(inner, call)
		}
	}
	if len(inner) != 2 || inner[0].Site() != inner[1].Site() || inner[0].Parent() == inner[1].Parent() {
		t.Fatal("fixture lost shared declaration or distinct parents")
	}
	first, _ := inner[0].Facts()
	second, _ := inner[1].Facts()
	if first.LookupEnvironmentUnknown || !second.LookupEnvironmentUnknown {
		t.Fatalf("platform-dependent facts reused: first=%+v second=%+v", first, second)
	}
}
