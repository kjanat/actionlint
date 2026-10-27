package composition

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

func TestPreparedScopeRetainsEarlierCheckoutPlacement(t *testing.T) {
	f := newInvocationFixture(t, t.Context(), "on: push\njobs:\n  test:\n    runs-on: windows-latest\n    steps:\n      - uses: actions/checkout@v4\n        with: {path: repo}\n      - uses: ./REPO/child\n      - uses: actions/checkout@v4\n        with: {path: Repo}\n", nil, nil)
	directory := filepath.Join(f.service.options.Paths.Workspace, "child")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "action.yml"), []byte("name: child\ndescription: child\nruns: {using: composite, steps: [{shell: bash, run: echo child}]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := f.service.Workflow(f.root)
	if err != nil {
		t.Fatal(err)
	}
	scopes := result.Batch.Scopes()
	if len(scopes) != 2 {
		t.Fatalf("expected entry and child, got %d scopes", len(scopes))
	}
	paths := scopes[1].Paths
	if paths.ActionPath != directory || paths.ActionRunnerPath != "repo/child" || paths.Placements == nil || !paths.Placements.CaseInsensitive || paths.Placements.Directory.Kind != execution.DirectoryKnown || paths.Placements.Directory.Path != "repo" {
		t.Fatalf("earlier checkout origin changed: %+v", paths)
	}
	current := f.service.options.Actions.CheckoutState()
	if current == nil || current.Directory.Path != "Repo" {
		t.Fatal("fixture did not advance checkout after the child")
	}
}

func TestStandaloneNestedCallRetainsUnknownPlatform(t *testing.T) {
	f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo unused}]}}\n", map[string]string{
		"outer/action.yml": "name: outer\ndescription: outer\nruns: {using: composite, steps: [{uses: $/inner, env: {Path: custom}}]}\n",
		"inner/action.yml": "name: inner\ndescription: inner\nruns: {using: composite, steps: [{shell: bash, run: echo inner}]}\n",
	}, nil)
	if f.service.options.Actions.Platform() != runnerlang.Any {
		t.Fatal("standalone entry fabricated a runner")
	}
	lookup, _ := f.cache.LookupMetadata("$/outer")
	result, err := standaloneActionForTest(t, f.service, lookup.Metadata())
	if err != nil {
		t.Fatal(err)
	}
	calls := result.Batch.Invocations().Calls()
	if len(calls) != 1 || calls[0].Uses() != "$/inner" {
		t.Fatal("standalone fixture lost nested call")
	}
	facts, exists := calls[0].Facts()
	if !exists || !facts.LookupEnvironmentUnknown || !facts.CheckoutEnvironmentUnknown || f.service.options.Actions.Platform() != runnerlang.Any {
		t.Fatalf("unknown platform lost conservative call facts: %+v", facts)
	}
}

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
	if fact, exists := result.Batch.Fact(0, false); !exists || invocation.ID(fact.Occurrence.Owner) != result.Batch.Invocations().RootID() || fact.Occurrence.Depth != 0 {
		t.Fatal("entry script fabricated a caller")
	}
}

func TestPreparedActionEntryRejectsForgedSourcePath(t *testing.T) {
	f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo root}]}}\n", map[string]string{
		"child/action.yml": "name: child\ndescription: child\nruns: {using: composite, steps: [{shell: bash, run: echo child}]}\n",
	}, nil)
	lookup, _ := f.cache.LookupMetadata("$/child")
	if lookup.Metadata() == nil {
		t.Fatal("fixture metadata absent")
	}
	metadata := *lookup.Metadata()
	metadata.SourceMapped = false
	declarations := newCapturedSnapshots(t, sourceFixtureBase(t))
	f.service.options.SourceSnapshots = declarations.SourceSnapshots
	result, err := standaloneActionForTest(t, f.service, &metadata, declarations)
	if err != nil {
		t.Fatal(err)
	}
	scopes, scripts := result.Batch.Snapshot()
	if len(scopes) != 1 || !scopes[0].Context.IsEntry() || scopes[0].Deployment.ActionPath() == "" || !scopes[0].Composite {
		t.Fatal("fixture did not create standalone composite entry")
	}
	scopes[0].Paths.ActionPath = "foreign"
	_, err = scriptexecution.NewBatch(result.Batch.Invocations(), scriptexecution.Collection{Declarations: declarations.SourceSnapshots, Scopes: scopes, Scripts: requestsFromUnits(scripts), Collected: true})
	if !errors.Is(err, scriptexecution.ErrInvalidBatch) || !strings.Contains(err.Error(), "action paths disagree") {
		t.Fatalf("forged action entry accepted: %v", err)
	}
}

func TestPreparedTopLevelCallHasNoExternalCaller(t *testing.T) {
	f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/child}]}}\n", map[string]string{
		"child/action.yml": "name: child\ndescription: child\nruns: {using: composite, steps: [{shell: bash, run: echo child}]}\n",
	}, nil)
	result, err := f.service.Workflow(f.root)
	if err != nil {
		t.Fatal(err)
	}
	fact, exists := result.Batch.Fact(0, false)
	if !exists {
		t.Fatal("top-level call projection absent")
	}
	graph := result.Batch.CallGraph()
	call, exists := graph.Call(fact.Occurrence.Owner)
	if !exists {
		t.Fatal("top-level owning call absent")
	}
	w, exists := f.root.Workflow()
	if !exists {
		t.Fatal("fixture workflow absent")
	}
	step := w.Jobs["test"].Steps[0]
	site, err := f.root.StepReference(step)
	if err != nil {
		t.Fatal(err)
	}
	if call.Parent != graph.Entry().ID || call.Site.Document != graph.Entry().Reference.Document || call.Depth != 1 || call.Uses != "$/child" || call.Site != site || call.Step.Line != step.Pos.Line || call.Step.Column != step.Pos.Col {
		t.Fatalf("top-level public call facts: %+v", call)
	}
}

func TestPreparedBatchRoutesRequestsByGraphOwner(t *testing.T) {
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
	graph := result.Batch.Invocations()
	configByOwner := map[invocation.ID]*repoconfig.Config{}
	for i, script := range scripts {
		record, exists := graph.Script(script.Occurrence)
		if !exists {
			t.Fatal("fixture script is absent from the graph")
		}
		config := &repoconfig.Config{}
		configByOwner[record.Owner] = config
		for _, scope := range scopes {
			if scope.Context.ID() == record.Owner {
				scope.Config = config
			}
		}
		if i == 1 && configByOwner[record.Owner] == configByOwner[graph.Scripts()[0].Owner] {
			t.Fatal("fixture calls did not have distinct owners")
		}
	}
	requests := requestsFromUnits(scripts)
	executionByOccurrence := make(map[invocation.ID]execution.Shell, len(requests))
	for i := range requests {
		name := "bash"
		if i == 1 {
			name = "pwsh"
		}
		requests[i].Execution.Shell = execution.Shell{Name: name}
		executionByOccurrence[requests[i].Occurrence] = requests[i].Execution.Shell
	}
	requests[0], requests[1] = requests[1], requests[0]
	scopes[1], scopes[2] = scopes[2], scopes[1]
	rebuilt, err := scriptexecution.NewBatch(graph, scriptexecution.Collection{
		Declarations: f.root.Registry(), Scopes: scopes, Scripts: requests, Collected: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, unit := range rebuilt.Scripts() {
		record, _ := graph.Script(unit.Occurrence)
		if unit.Config != configByOwner[record.Owner] || unit.Config != unit.Scope.Config {
			t.Fatalf("request for owner %q used another invocation config", record.Owner)
		}
		if unit.Execution.Shell != executionByOccurrence[unit.Occurrence] {
			t.Fatalf("request for occurrence %q used another execution", unit.Occurrence)
		}
	}
}

func TestStandaloneNonCompositeEntriesKeepBoundDeployment(t *testing.T) {
	for _, collect := range []bool{false, true} {
		for _, using := range []string{"node24", "docker"} {
			t.Run(fmt.Sprintf("%s/collect=%t", using, collect), func(t *testing.T) {
				body := "main: index.js"
				if using == "docker" {
					body = "image: Dockerfile"
				}
				meta, err := actionmeta.Parse("source/action.yml", []byte("name: action\ndescription: test\nruns:\n  using: "+using+"\n  "+body+"\n"))
				if err != nil {
					t.Fatal(err)
				}
				declarations := newCapturedSnapshots(t, sourceFixtureBase(t))
				service := New(Options{Context: t.Context(), Actions: actionlocal.NewResolver(nil, nil), SourceSnapshots: declarations.SourceSnapshots, CollectScripts: collect})
				result, err := standaloneActionForTest(t, service, meta, declarations)
				if err != nil {
					t.Fatal(err)
				}
				scopes := result.Batch.Scopes()
				if !result.Batch.Valid() || len(scopes) != 1 || !scopes[0].Context.IsEntry() || scopes[0].Composite || scopes[0].Deployment.SourcePath() != meta.Path() || scopes[0].Deployment.ActionPath() != filepath.Dir(meta.Path()) || len(result.Batch.Scripts()) != 0 {
					t.Fatalf("non-composite entry lost its deployment or added scripts: scopes=%+v scripts=%+v", scopes, result.Batch.Scripts())
				}
			})
		}
	}
}

type standalonePreHookFailure struct {
	workflowvisit.Base
	failure error
}

func (rule *standalonePreHookFailure) VisitWorkflowPre(*workflowlang.Workflow) error {
	return rule.failure
}

func TestStandalonePreHookFailureKeepsValidDeployment(t *testing.T) {
	meta, err := actionmeta.Parse("source/action.yml", []byte("name: action\ndescription: test\nruns: {using: composite, steps: []}\n"))
	if err != nil {
		t.Fatal(err)
	}
	declarations := newCapturedSnapshots(t, sourceFixtureBase(t))
	failure := errors.New("pre-hook failure")
	rule := &standalonePreHookFailure{Base: workflowvisit.NewBase("pre-hook", ""), failure: failure}
	service := New(Options{
		Context: t.Context(), Actions: actionlocal.NewResolver(nil, nil), SourceSnapshots: declarations.SourceSnapshots,
		CollectScripts: true, Rules: []workflowvisit.Rule{rule},
	})
	result, err := standaloneActionForTest(t, service, meta, declarations)
	if !errors.Is(err, failure) || !result.Batch.Valid() || len(result.Batch.Scripts()) != 0 {
		t.Fatalf("pre-hook failure or prepared action batch lost: error=%v result=%+v", err, result)
	}
	scopes := result.Batch.Scopes()
	if len(scopes) != 1 || scopes[0].Composite || scopes[0].Deployment.SourcePath() != meta.Path() || scopes[0].Deployment.ActionPath() != filepath.Dir(meta.Path()) {
		t.Fatalf("pre-hook error left an invalid action entry scope: %+v", scopes)
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
			if _, err := scriptexecution.NewBatch(first.Batch.Invocations(), scriptexecution.Collection{Declarations: f.root.Registry(), Scopes: scopes, Scripts: requestsFromUnits(units), Collected: true}); !errors.Is(err, scriptexecution.ErrInvalidBatch) || !strings.Contains(err.Error(), want) {
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
	owner, exists := graph.Call(graph.Scripts()[0].Owner)
	if !exists || owner.Uses() != "$/inner" || owner.Depth() != 2 {
		t.Fatal("nested script lost its owning call")
	}
	projection, exists := result.Batch.Fact(0, false)
	if !exists {
		t.Fatal("missing invocation projection")
	}
	call, exists := result.Batch.CallGraph().Call(projection.Occurrence.Owner)
	if !exists || invocation.ID(projection.Occurrence.Owner) != owner.ID() {
		t.Fatal("missing occurrence owner call")
	}
	prefix, _, found := strings.Cut(line, "{uses")
	if !found {
		t.Fatal("fixture has no call mapping")
	}
	wantColumn := utf8.RuneCountInString(prefix) + 1
	if call.Site != owner.Site() || !strings.HasSuffix(filepath.ToSlash(call.Site.Document), "/outer/action.yml") || call.Step.Line != 1 || call.Step.Column != wantColumn || call.Depth != 2 || call.Uses != "$/inner" || invocation.ID(call.Parent) != owner.Parent() {
		t.Fatalf("public projection lost canonical call facts: %+v", call)
	}
}

func TestSharedCallDeclarationRetainsPlatformSpecificFacts(t *testing.T) {
	f := newInvocationFixture(t, t.Context(), "on: push\njobs:\n  linux:\n    runs-on: ubuntu-latest\n    steps: [{uses: $/outer}]\n  windows:\n    runs-on: windows-latest\n    steps: [{uses: $/outer}]\n", map[string]string{
		"outer/action.yml": "name: outer\ndescription: outer\nruns: {using: composite, steps: [{uses: $/inner, env: {Path: custom}}]}\n",
		"inner/action.yml": "name: inner\ndescription: inner\nruns: {using: composite, steps: [{shell: bash, run: echo inner}]}\n",
	}, nil)
	_, available := f.root.Workflow()
	declaration, declared := f.root.WorkflowView()
	linux, linuxPresent := declaration.Job("linux")
	windows, windowsPresent := declaration.Job("windows")
	if !available || !declared || !linuxPresent || !windowsPresent ||
		f.service.options.Runners.Job(linux).Platform != runnerlang.MacOrLinux || f.service.options.Runners.Job(windows).Platform != runnerlang.Windows {
		t.Fatal("fixture did not resolve distinct runner platforms")
	}
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
	if first.LookupEnvironmentUnknown || !second.LookupEnvironmentUnknown || first.CheckoutEnvironmentUnknown || !second.CheckoutEnvironmentUnknown {
		t.Fatalf("platform-dependent facts reused: first=%+v second=%+v", first, second)
	}
}
