package composition

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/builtin"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
	"actionlint.kjanat.dev/v2/source"
)

type invocationFixture struct {
	service    *Service
	root       *documentparse.SourceSnapshot
	snapshots  *documentparse.SourceSnapshots
	cache      *actionlocal.Cache
	parsed     []string
	afterParse func(string)
}

func newInvocationFixture(t *testing.T, ctx context.Context, workflow string, actions map[string]string, budget *invocation.Budget) *invocationFixture {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "ci.yml")
	content := []byte(workflow)
	document := documentparse.Workflow(path, content)
	if len(document.Findings) != 0 {
		t.Fatal(document.Findings)
	}
	f := &invocationFixture{snapshots: &documentparse.SourceSnapshots{}, cache: actionlocal.NewCache(directory, nil)}
	var err error
	f.root, err = f.snapshots.Document(fsread.PhysicalPath(path), content, document)
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string][]byte{}
	for path, content := range actions {
		sources[fsread.PhysicalPath(filepath.Join(directory, filepath.FromSlash(path)))] = []byte(content)
	}
	f.cache.Configure(actionlocal.Options{Sources: sources, OnDocument: func(path string, _ *actionmeta.Metadata, _ error) {
		f.parsed = append(f.parsed, path)
		if f.afterParse != nil {
			f.afterParse(path)
		}
	}})
	resolver := actionlocal.NewResolver(f.cache, nil)
	f.service = New(Options{Context: ctx, SourceSnapshots: f.snapshots, Actions: resolver, Budget: budget, CollectScripts: true, Paths: execution.Paths{Workspace: directory, Analysis: directory}, Rules: []workflowvisit.Rule{builtin.NewRuleAction(resolver)}})
	return f
}

func (f *invocationFixture) actionSnapshot(t *testing.T, spec string) *documentparse.SourceSnapshot {
	t.Helper()
	result, _ := f.cache.LookupMetadata(spec)
	if result.Metadata() == nil {
		t.Fatalf("metadata unavailable for %s: %+v", spec, result)
	}
	snapshot, err := f.snapshots.Action(fsread.PhysicalPath(result.Metadata().Path()), result.Metadata())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestComposedInvocationChainsFollowStructuralOwners(t *testing.T) {
	workflow := `on: push
jobs:
  first:
    runs-on: ubuntu-latest
    steps:
      - &outer
        uses: $/outer
      - *outer
  second:
    runs-on: ubuntu-latest
    steps:
      - uses: $/outer
`
	for _, middle := range []bool{false, true} {
		name := "two-level"
		if middle {
			name = "three-level"
		}
		t.Run(name, func(t *testing.T) {
			aliased := "name: calls\ndescription: identity\nruns:\n  using: composite\n  steps:\n    - &nested\n      uses: $/nested\n    - *nested\n"
			actions := map[string]string{
				"outer/action.yml":  aliased,
				"nested/action.yml": "name: nested\ndescription: identity\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo nested\n",
			}
			if middle {
				actions["outer/action.yml"] = "name: outer\ndescription: identity\nruns:\n  using: composite\n  steps:\n    - uses: $/middle\n"
				actions["middle/action.yml"] = aliased
			}
			f := newInvocationFixture(t, t.Context(), workflow, actions, nil)
			result, err := f.service.Workflow(f.root)
			if err != nil {
				t.Fatal(err)
			}
			graph := result.Batch.Invocations
			if graph.Incomplete() || len(result.Batch.Scripts) != 6 {
				t.Fatalf("incomplete=%v scripts=%d", graph.Incomplete(), len(result.Batch.Scripts))
			}
			if len(graph.Scripts()) != len(result.Batch.Scripts) {
				t.Fatal("traversal bypassed the canonical script owner")
			}
			for _, script := range result.Batch.Scripts {
				registered, exists := graph.Script(script.Occurrence)
				if !exists || registered.Declaration != script.Reference || registered.Owner != script.Scope.Occurrence {
					t.Fatalf("script occurrence differs from its owning graph: %+v", script)
				}
			}
			root, _ := f.root.Workflow()
			var rootSites []source.Reference
			for _, step := range slices.Concat(root.Jobs["first"].Steps, root.Jobs["second"].Steps) {
				ref, err := f.root.StepReference(step)
				if err != nil {
					t.Fatal(err)
				}
				rootSites = append(rootSites, ref)
			}
			outer := f.actionSnapshot(t, "$/outer")
			outerSteps, _ := outer.ActionSteps()
			leaf := f.actionSnapshot(t, "$/nested")
			leafSteps, _ := leaf.ActionSteps()
			declaration, err := leaf.StepReference(leafSteps[0])
			if err != nil {
				t.Fatal(err)
			}
			seen := map[invocation.ID]bool{}
			for i, unit := range result.Batch.Scripts {
				chain := []invocation.ID{graph.RootID()}
				sites := []source.Reference{rootSites[i/2]}
				if middle {
					ref, err := outer.StepReference(outerSteps[0])
					if err != nil {
						t.Fatal(err)
					}
					sites = append(sites, ref)
					middleSnapshot := f.actionSnapshot(t, "$/middle")
					steps, _ := middleSnapshot.ActionSteps()
					ref, err = middleSnapshot.StepReference(steps[i%2])
					if err != nil {
						t.Fatal(err)
					}
					sites = append(sites, ref)
				} else {
					ref, err := outer.StepReference(outerSteps[i%2])
					if err != nil {
						t.Fatal(err)
					}
					sites = append(sites, ref)
				}
				for _, site := range sites {
					id, err := invocation.NewCallID(chain[len(chain)-1], site)
					if err != nil {
						t.Fatal(err)
					}
					chain = append(chain, id)
				}
				actual, exists := graph.Chain(unit.Scope.Occurrence)
				if !exists || !reflect.DeepEqual(actual, chain) {
					t.Fatalf("script %d chain %v, want %v", i, actual, chain)
				}
				path, _ := graph.Path(unit.Scope.Occurrence)
				for edge, call := range path {
					if call.Site != sites[edge] || call.Parent != chain[edge] || call.Depth != edge+1 {
						t.Fatalf("script %d edge %d: %+v", i, edge, call)
					}
				}
				want, err := invocation.NewScriptID(unit.Scope.Occurrence, declaration, invocation.ScriptRun, "")
				if err != nil || unit.Reference != declaration || unit.Occurrence != want || seen[unit.Occurrence] {
					t.Fatalf("script %d lost declaration/occurrence identity: %+v, %v", i, unit, err)
				}
				seen[unit.Occurrence] = true
			}
		})
	}
}

func TestStandaloneEntryCycleHasNoSyntheticCall(t *testing.T) {
	f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo unused}]}}\n", map[string]string{"self/action.yml": "name: self\ndescription: identity\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo root\n    - uses: $/self\n"}, nil)
	metadata, _ := f.cache.LookupMetadata("$/self")
	result, err := f.service.Action(metadata.Metadata())
	if err != nil {
		t.Fatal(err)
	}
	graph := result.Batch.Invocations
	if graph.Entry().Kind != invocation.EntryAction || len(graph.Calls()) != 1 || len(result.Batch.Scopes) != 1 || len(result.Batch.Scripts) != 1 {
		t.Fatalf("fabricated standalone caller: %+v", result.Batch)
	}
	call := graph.Calls()[0]
	cycle, ok := call.Outcome.Cycle()
	if !ok || cycle.Owner != graph.RootID() || call.Depth != 1 || call.Parent != graph.RootID() || !graph.Incomplete() {
		t.Fatalf("standalone cycle: %+v", call)
	}
	if result.Batch.Scripts[0].Scope.Occurrence != graph.RootID() {
		t.Fatal("root script attached to a fabricated call")
	}
}

func TestCompositionBudgetsGateMetadataValidationLookups(t *testing.T) {
	for _, tc := range []struct {
		name   string
		budget invocation.Budget
		total  bool
	}{
		{"depth", invocation.Budget{MaxDepth: 1, MaxCalls: 10, MaxScripts: invocation.DefaultMaxScripts}, false},
		{"total", invocation.Budget{MaxDepth: 10, MaxCalls: 1, MaxScripts: invocation.DefaultMaxScripts}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/outer}]}}\n", map[string]string{
				"outer/action.yml":  "name: outer\ndescription: identity\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo retained\n    - uses: $/nested\n",
				"nested/action.yml": "name: nested\ndescription: identity\nruns: {using: composite, steps: []}\n",
			}, &tc.budget)
			result, err := f.service.Workflow(f.root)
			if errors.Is(err, invocation.ErrCallBudget) != tc.total || err != nil && !tc.total {
				t.Fatal(err)
			}
			if len(f.parsed) != 1 || filepath.Base(filepath.Dir(f.parsed[0])) != "outer" {
				t.Fatalf("validation fetched a body beyond the budget: %v", f.parsed)
			}
			if !result.Batch.Invocations.Incomplete() || len(result.Batch.Scripts) != 1 || result.Batch.Scripts[0].Run.Run.Value != "echo retained" {
				t.Fatal("budget lost prior facts or incomplete state")
			}
			if tc.total {
				if _, exists := result.Batch.Invocations.Stop(); !exists {
					t.Fatal("missing total stop boundary")
				}
			}
		})
	}
}

func TestRepeatedMetadataFailuresRemainUnresolvedCalls(t *testing.T) {
	f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/broken}, {uses: $/broken}]}}\n", map[string]string{"broken/action.yml": "runs: [\n"}, nil)
	result, err := f.service.Workflow(f.root)
	if err != nil {
		t.Fatal(err)
	}
	calls := result.Batch.Invocations.Calls()
	if len(calls) != 2 || !result.Batch.Invocations.Incomplete() {
		t.Fatalf("unresolved calls lost: %+v", calls)
	}
	for _, call := range calls {
		if call.Outcome.Kind() != invocation.OutcomeUnresolved || call.Outcome.Reason() != invocation.ReasonParseFailed || call.Outcome.Detail() == "" {
			t.Fatalf("cached failure became opaque: %+v", call)
		}
	}
	if calls[0].Outcome.Detail() != calls[1].Outcome.Detail() {
		t.Fatal("repeated call lost original cause")
	}
}

func TestInvalidAndDynamicCallsRetainDistinctOutcomes(t *testing.T) {
	f := newInvocationFixture(t, t.Context(), `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: unversioned
      - uses: $/source@v1
      - uses: ${{ inputs.action }}
`, nil, nil)
	result, err := f.service.Workflow(f.root)
	if err != nil {
		t.Fatal(err)
	}
	calls := result.Batch.Invocations.Calls()
	if len(calls) != 3 || len(f.parsed) != 0 {
		t.Fatalf("invalid/dynamic calls acquired metadata: calls=%d parsed=%v", len(calls), f.parsed)
	}
	for _, call := range calls[:2] {
		if call.Outcome.Kind() != invocation.OutcomeUnresolved || call.Outcome.Reason() != invocation.ReasonInvalidCall {
			t.Fatalf("invalid reference reported as unavailable: %+v", call)
		}
	}
	if calls[2].Outcome.Kind() != invocation.OutcomeDynamic {
		t.Fatalf("dynamic reference reported as invalid: %+v", calls[2])
	}
}

func TestCompositionCancellationRetainsEntryOrPendingCall(t *testing.T) {
	for _, pending := range []bool{false, true} {
		name := "entry"
		if pending {
			name = "pending-call"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			f := newInvocationFixture(t, ctx, "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/outer}]}}\n", map[string]string{"outer/action.yml": "name: outer\ndescription: identity\nruns: {using: composite, steps: []}\n"}, nil)
			if pending {
				f.afterParse = func(string) { cancel() }
			} else {
				cancel()
			}
			result, err := f.service.Workflow(f.root)
			if !errors.Is(err, context.Canceled) || !result.Batch.Invocations.Incomplete() {
				t.Fatalf("cancellation lost: %v", err)
			}
			calls := result.Batch.Invocations.Calls()
			if pending {
				if len(calls) != 1 || calls[0].Outcome.Kind() != invocation.OutcomeInterrupted || calls[0].Outcome.Reason() != invocation.ReasonCanceled {
					t.Fatalf("pending call lost: %+v", calls)
				}
			} else {
				if _, exists := result.Batch.Invocations.EntryInterruption(); !exists || len(calls) != 0 {
					t.Fatal("entry interruption lost")
				}
			}
		})
	}
}
