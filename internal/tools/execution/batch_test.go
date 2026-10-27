package execution

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowexecution "actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	"actionlint.kjanat.dev/v2/source"
)

func TestBatchRejectsUnboundSource(t *testing.T) {
	for _, change := range []string{"missing registry", "missing document", "stale revision", "foreign document"} {
		t.Run(change, func(t *testing.T) {
			graph, collection := batchFixture(t, false)
			snapshot, _ := collection.Declarations.Lookup(graph.Entry().Reference)
			content, identity := snapshot.Content(), "ci.yml"
			declarations := newCapturedSnapshots(t, sourceFixtureBase(t))
			collection.Declarations = declarations.SourceSnapshots
			switch change {
			case "missing registry":
				collection.Declarations = nil
			case "stale revision":
				content = append(content, []byte("# changed\n")...)
			case "foreign document":
				identity = "unregistered.yml"
			}
			if change == "stale revision" || change == "foreign document" {
				if _, err := declarations.Document(identity, content, documentparse.Workflow(identity, content)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := NewBatch(graph, collection); !errors.Is(err, ErrInvalidBatch) {
				t.Fatalf("unregistered source accepted: %v", err)
			}
		})
	}
}

func TestBatchValidatesScopeOutcome(t *testing.T) {
	for _, state := range []string{"depth", "cycle", "pending interrupted", "interrupted", "noncomposite"} {
		t.Run(state, func(t *testing.T) {
			entry := source.Reference{Document: "entry", Node: "document"}
			builder, err := invocation.NewBuilder(invocation.EntryAction, entry, invocation.Budget{MaxDepth: 1, MaxCalls: 5, MaxScripts: 5})
			if err != nil {
				t.Fatal(err)
			}
			scopes := []*Scope{{Context: builder.RootContext(), Composite: true, Deployment: Deployment{reference: entry}}}
			ticket, err := builder.Begin(builder.RootContext(), source.Reference{Document: "entry", Node: "call"}, invocation.Facts{Condition: invocation.ConditionEnabled})
			if err != nil {
				t.Fatal(err)
			}
			call, admitted := ticket.Context()
			if !admitted {
				t.Fatal("call was not admitted")
			}
			target := source.Reference{Document: "child", Node: "document"}
			if state == "cycle" {
				target = entry
			}
			outcome, err := invocation.Expanded(target)
			if err != nil {
				t.Fatal(err)
			}
			if state != "pending interrupted" {
				if _, err := builder.Resolve(call, outcome); err != nil {
					t.Fatal(err)
				}
			}
			scopes = append(scopes, &Scope{Context: call, Composite: state != "noncomposite", Deployment: Deployment{reference: target}})
			if state == "depth" {
				frontier, err := builder.Begin(call, source.Reference{Document: "child", Node: "nested"}, invocation.Facts{Condition: invocation.ConditionEnabled})
				if err != nil || frontier.Status() != invocation.DepthLimited {
					t.Fatalf("depth fixture: %v", err)
				}
				context, exists := builder.Context(frontier.ID())
				if !exists {
					t.Fatal("frontier context absent")
				}
				scopes = append(scopes, &Scope{Context: context, Composite: true})
			}
			if state == "interrupted" || state == "pending interrupted" {
				if _, err := builder.Interrupt(invocation.ReasonCanceled, "cancelled"); err != nil {
					t.Fatal(err)
				}
			}
			if state != "cycle" {
				if err := builder.Close(call); err != nil {
					t.Fatal(err)
				}
			}
			graph, err := builder.Publish()
			if err != nil {
				t.Fatal(err)
			}
			batch, err := NewBatch(graph, Collection{Scopes: scopes})
			if state == "interrupted" {
				if err != nil || !batch.Valid() {
					t.Fatalf("interrupted body rejected: %v", err)
				}
			} else {
				want := "call scope has no resolved body"
				switch state {
				case "cycle":
					want = "call scope outcome is not executable"
				case "noncomposite":
					want = "call scope must be composite"
				}
				if !errors.Is(err, ErrInvalidBatch) || !strings.Contains(err.Error(), want) {
					t.Fatalf("invalid %s scope: %v", state, err)
				}
			}
		})
	}
}

func TestBatchRejectsForgedEntryPaths(t *testing.T) {
	for _, field := range []string{"analysis path", "declared analysis path", "runner path", "independent"} {
		t.Run(field, func(t *testing.T) {
			graph, collection := batchFixture(t, false)
			scopes := collection.Scopes
			want := "action paths disagree"
			switch field {
			case "analysis path":
				scopes[0].Paths.ActionPath = "foreign"
			case "declared analysis path":
				scopes[0].Deployment.actionPath, scopes[0].Paths.ActionPath = "foreign", "foreign"
				want = "workflow entry cannot have a deployment"
			case "runner path":
				scopes[0].Paths.ActionRunnerPath = "foreign"
			case "independent":
				scopes[0].Paths.ActionIndependent = true
			}
			_, err := NewBatch(graph, collection)
			if !errors.Is(err, ErrInvalidBatch) || !strings.Contains(err.Error(), want) {
				t.Fatalf("forged entry %s: %v", field, err)
			}
		})
	}
}

func batchFixture(t *testing.T, withInput bool) (invocation.Graph, Collection) {
	t.Helper()
	content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok # original\n")
	if withInput {
		content = append(content, []byte("      - uses: actions/github-script@v7\n        with: {script: console.log(1)}\n")...)
	}
	return batchSourceFixture(t, content)
}

func batchSourceFixture(t *testing.T, content []byte) (invocation.Graph, Collection) {
	t.Helper()
	parsed := documentparse.Workflow("ci.yml", content)
	if len(parsed.Findings) != 0 {
		t.Fatal(parsed.Findings)
	}
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document("ci.yml", content, parsed)
	if err != nil {
		t.Fatal(err)
	}
	builder, err := invocation.NewBuilder(invocation.EntryWorkflow, snapshot.Reference(), invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	scope := &Scope{Context: builder.RootContext()}
	var requests []ScriptRequest
	for _, step := range parsed.Workflow.Jobs["test"].Steps {
		ref, err := snapshot.StepReference(step)
		if err != nil {
			t.Fatal(err)
		}
		request := ScriptRequest{}
		kind, input := invocation.ScriptRun, ""
		switch step.Exec.(type) {
		case *workflowlang.ExecRun:
		case *workflowlang.ExecAction:
			kind, input = invocation.ScriptActionInput, "script"
			request.Input = &analyzer.ScriptInput{Action: "actions/github-script", Input: input, Language: "javascript"}
		default:
			t.Fatal("unexpected fixture step")
		}
		request.Occurrence, err = builder.AddScript(builder.RootContext(), ref, kind, input)
		if err != nil {
			t.Fatal(err)
		}
		requests = append(requests, request)
	}
	graph, err := builder.Publish()
	if err != nil {
		t.Fatal(err)
	}
	return graph, Collection{Declarations: snapshots.SourceSnapshots, Scopes: []*Scope{scope}, Scripts: requests, Collected: true}
}

func TestBatchRejectsForeignInputsAndIsolatesLegacyMutations(t *testing.T) {
	for name, mutate := range map[string]func(*invocation.Graph, *Collection, *workflowlang.ExecRun, *workflowlang.Input){
		"missing run mapping": func(_ *invocation.Graph, _ *Collection, r *workflowlang.ExecRun, _ *workflowlang.Input) {
			r.Source = nil
		},
		"missing run position": func(_ *invocation.Graph, _ *Collection, r *workflowlang.ExecRun, _ *workflowlang.Input) {
			r.Run.Pos = nil
		},
		"missing run key": func(_ *invocation.Graph, _ *Collection, r *workflowlang.ExecRun, _ *workflowlang.Input) {
			r.RunPos = nil
		},
		"missing input mapping": func(_ *invocation.Graph, _ *Collection, _ *workflowlang.ExecRun, i *workflowlang.Input) {
			i.Source = nil
		},
		"missing input position": func(_ *invocation.Graph, _ *Collection, _ *workflowlang.ExecRun, i *workflowlang.Input) {
			i.Value.Pos = nil
		},
		"missing input key": func(_ *invocation.Graph, _ *Collection, _ *workflowlang.ExecRun, i *workflowlang.Input) {
			i.Name.Pos = nil
		},
		"unpublished graph": func(g *invocation.Graph, _ *Collection, _ *workflowlang.ExecRun, _ *workflowlang.Input) {
			*g = invocation.Graph{}
		},
		"nil scope": func(_ *invocation.Graph, c *Collection, _ *workflowlang.ExecRun, _ *workflowlang.Input) {
			c.Scopes[0] = nil
		},
		"unknown scope": func(_ *invocation.Graph, c *Collection, _ *workflowlang.ExecRun, _ *workflowlang.Input) {
			c.Scopes[0].Context = invocation.Context{}
		},
		"foreign occurrence": func(_ *invocation.Graph, c *Collection, _ *workflowlang.ExecRun, _ *workflowlang.Input) {
			c.Scripts[0].Occurrence = "unknown"
		},
		"duplicate occurrence": func(_ *invocation.Graph, c *Collection, _ *workflowlang.ExecRun, _ *workflowlang.Input) {
			c.Scripts[1] = c.Scripts[0]
		},
		"missing body": func(_ *invocation.Graph, _ *Collection, r *workflowlang.ExecRun, _ *workflowlang.Input) { r.Run = nil },
		"two bodies": func(_ *invocation.Graph, c *Collection, _ *workflowlang.ExecRun, _ *workflowlang.Input) {
			c.Scripts[0].Input = c.Scripts[1].Input
		},
		"missing input name": func(_ *invocation.Graph, _ *Collection, _ *workflowlang.ExecRun, i *workflowlang.Input) { i.Name = nil },
		"wrong input": func(_ *invocation.Graph, c *Collection, _ *workflowlang.ExecRun, _ *workflowlang.Input) {
			c.Scripts[1].Input.Input = "other"
		},
		"wrong action": func(_ *invocation.Graph, c *Collection, _ *workflowlang.ExecRun, _ *workflowlang.Input) {
			c.Scripts[1].Input.Action = "other/action"
		},
		"missing selector": func(_ *invocation.Graph, c *Collection, _ *workflowlang.ExecRun, _ *workflowlang.Input) {
			c.Scripts[1].Input = nil
		},
		"invalid selector": func(_ *invocation.Graph, c *Collection, _ *workflowlang.ExecRun, _ *workflowlang.Input) {
			c.Scripts[1].Input.Language = ""
		},
		"input shell": func(_ *invocation.Graph, c *Collection, _ *workflowlang.ExecRun, _ *workflowlang.Input) {
			c.Scripts[1].DefaultShell = new("bash")
		},
		"missing unit": func(_ *invocation.Graph, c *Collection, _ *workflowlang.ExecRun, _ *workflowlang.Input) {
			c.Scripts = c.Scripts[:1]
		},
	} {
		t.Run(name, func(t *testing.T) {
			graph, collection := batchFixture(t, true)
			run, input := fixtureBodies(t, graph, collection)
			baseline, err := NewBatch(graph, collection)
			if err != nil {
				t.Fatal(err)
			}
			originalRun, _ := baseline.Fact(0, false)
			originalInput, _ := baseline.Fact(1, false)
			mutate(&graph, &collection, run, input)
			batch, err := NewBatch(graph, collection)
			switch name {
			case "missing run mapping", "missing run position", "missing run key", "missing input mapping", "missing input position", "missing input key", "missing body", "missing input name":
				if err != nil {
					t.Fatalf("legacy mutation contaminated canonical declaration: %v", err)
				}
				actualRun, _ := batch.Fact(0, false)
				actualInput, _ := batch.Fact(1, false)
				if !reflect.DeepEqual(originalRun, actualRun) || !reflect.DeepEqual(originalInput, actualInput) {
					t.Fatal("legacy mutation changed canonical body, mapping, or provenance")
				}
				return
			}
			if !errors.Is(err, ErrInvalidBatch) || batch.Valid() {
				t.Fatalf("invalid input bound: %v", err)
			}
		})
	}
}

func TestBatchRejectsUnmappedParsedScriptSource(t *testing.T) {
	for _, tc := range []struct {
		name, identity, content string
		kind                    invocation.EntryKind
		scriptKind              invocation.ScriptKind
		input                   string
	}{
		{
			name:       "run",
			identity:   "ci.yml",
			content:    "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: {script: echo ok}\n",
			kind:       invocation.EntryWorkflow,
			scriptKind: invocation.ScriptRun,
		},
		{
			name:       "action input",
			identity:   "action.yml",
			content:    "name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: actions/github-script@v7\n      with:\n        script: {source: console.log(1)}\n",
			kind:       invocation.EntryAction,
			scriptKind: invocation.ScriptActionInput,
			input:      "script",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := []byte(tc.content)
			var document documentparse.Document
			if tc.kind == invocation.EntryWorkflow {
				document = documentparse.Workflow(tc.identity, content)
			} else {
				document = documentparse.Action(tc.identity, tc.identity, content)
			}
			if !document.CanAnalyze() {
				t.Fatalf("parsed fixture cannot be analyzed: %v", document.Findings)
			}
			registry := newCapturedSnapshots(t, sourceFixtureBase(t))
			snapshot, err := registry.Document(tc.identity, content, document)
			if err != nil {
				t.Fatal(err)
			}
			facts := document.Facts()
			job := "test"
			if tc.kind == invocation.EntryAction {
				job = ""
			}
			var declaration *documentparse.StepView
			for _, step := range facts.Jobs[job].Steps {
				reference, err := snapshot.StepReference(step)
				if err != nil {
					t.Fatal(err)
				}
				candidate, exists := registry.Declaration(reference)
				if !exists {
					t.Fatalf("canonical declaration %q is missing", reference)
				}
				if tc.scriptKind == invocation.ScriptRun {
					run, exists := candidate.Run()
					if exists {
						if run.Source().Present() {
							t.Fatal("non-scalar run unexpectedly has a scalar source mapping")
						}
						declaration = &candidate
						break
					}
				} else if action, exists := candidate.Action(); exists {
					declaration = &candidate
					inputs, _ := action.InputNames()
					for name, input := range inputs {
						if name == tc.input {
							if input.Source().Present() {
								t.Fatal("non-scalar action input unexpectedly has a scalar source mapping")
							}
						}
					}
					break
				}
			}
			if declaration == nil {
				t.Fatalf("fixture has no canonical script declaration: steps=%#v metadata=%#v findings=%v", facts.Jobs[job].Steps, document.Metadata, document.Findings)
			}
			builder, err := invocation.NewBuilder(tc.kind, snapshot.Reference(), invocation.DefaultBudget())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := builder.AddScript(builder.RootContext(), declaration.Reference(), tc.scriptKind, tc.input); err != nil {
				t.Fatal(err)
			}
			graph, err := builder.Publish()
			if err != nil {
				t.Fatal(err)
			}
			scope := &Scope{Context: builder.RootContext(), Composite: tc.kind == invocation.EntryAction}
			if tc.kind == invocation.EntryAction {
				scope.Deployment, err = NewDeployment(snapshot, registry.Sources.Path(tc.identity), tc.identity, false)
				if err != nil {
					t.Fatal(err)
				}
				workflowexecution.ActionOrigin(&scope.Paths, scope.Context, scope.Deployment.ActionPath())
			}
			request := ScriptRequest{Occurrence: graph.Scripts()[0].ID}
			if tc.scriptKind == invocation.ScriptActionInput {
				request.Input = &analyzer.ScriptInput{Action: "actions/github-script", Input: tc.input, Language: "javascript"}
			}
			collection := Collection{Declarations: registry.SourceSnapshots, Scopes: []*Scope{scope}, Scripts: []ScriptRequest{request}, Collected: true}
			if _, err := NewBatch(graph, collection); !errors.Is(err, ErrInvalidBatch) {
				t.Fatalf("unmapped parsed %s accepted: %v", tc.name, err)
			}
		})
	}
}

func TestParsedScriptMappingsCarryRequiredPositions(t *testing.T) {
	graph, collection := batchFixture(t, true)
	for _, record := range graph.Scripts() {
		declaration, exists := collection.Declarations.Declaration(record.Declaration)
		if !exists {
			t.Fatalf("declaration %q is missing", record.Declaration)
		}
		if record.Kind == invocation.ScriptRun {
			run, isRun := declaration.Run()
			_, hasBodyPosition := run.Run().Pos()
			_, hasKeyPosition := run.RunPos()
			if !isRun || !run.Source().Present() || !hasBodyPosition || !hasKeyPosition {
				t.Fatalf("scalar run lacks parser-owned mapping or positions: run=%v body=%v key=%v", isRun, hasBodyPosition, hasKeyPosition)
			}
			continue
		}
		action, isAction := declaration.Action()
		if !isAction {
			t.Fatal("action input declaration is not an action")
		}
		inputs, _ := action.InputNames()
		var input documentparse.InputView
		var inputExists bool
		for name, candidate := range inputs {
			if name == record.Input {
				input, inputExists = candidate, true
				break
			}
		}
		_, hasNamePosition := input.Name().Pos()
		_, hasValuePosition := input.Value().Pos()
		if !inputExists || !input.Name().Present() || !input.Value().Present() || !input.Source().Present() || !hasNamePosition || !hasValuePosition {
			t.Fatalf("scalar input lacks parser-owned mapping or positions: found=%v name=%v value=%v source=%v", inputExists, hasNamePosition, hasValuePosition, input.Source().Present())
		}
	}
}

func fixtureBodies(t *testing.T, graph invocation.Graph, collection Collection) (*workflowlang.ExecRun, *workflowlang.Input) {
	t.Helper()
	snapshot, found := collection.Declarations.Lookup(graph.Entry().Reference)
	if !found {
		t.Fatal("fixture entry snapshot absent")
	}
	workflow, available := snapshot.Workflow()
	if !available || len(workflow.Jobs["test"].Steps) != 2 {
		t.Fatal("fixture mutable traversal absent")
	}
	steps := workflow.Jobs["test"].Steps
	for index, step := range steps {
		reference, err := snapshot.StepReference(step)
		if err != nil || reference != graph.Scripts()[index].Declaration {
			t.Fatalf("fixture cursor does not match script %d: %v", index, err)
		}
	}
	run, ok := steps[0].Exec.(*workflowlang.ExecRun)
	if !ok {
		t.Fatal("fixture run has wrong kind")
	}
	action, ok := steps[1].Exec.(*workflowlang.ExecAction)
	if !ok {
		t.Fatal("fixture input has wrong kind")
	}
	return run, action.Inputs["script"]
}

func TestBatchSnapshotsOwnMutableScriptValues(t *testing.T) {
	graph, collection := batchFixture(t, true)
	scopes := collection.Scopes
	run, argument := fixtureBodies(t, graph, collection)
	placement := workflowexecution.Checkout{Directory: workflowexecution.Directory{Kind: workflowexecution.DirectoryKnown, Path: "original"}}
	scopes[0].Paths.Placements = workflowexecution.NewCheckoutPlacement(placement, workflowexecution.CheckoutPlacement{})
	batch, err := NewBatch(graph, collection)
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
	assertBatchFactsMatchCanonicalDeclarations(t, batch, collection.Declarations)
	run.Run.Value = "corrupt original"
	run.Source.Spans[0].YamlLine = 999
	argument.Value.Value = "corrupt input"
	snapshot, exists := collection.Declarations.Lookup(graph.Entry().Reference)
	if !exists {
		t.Fatal("fixture entry snapshot absent")
	}
	workflow, available := snapshot.Workflow()
	if !available {
		t.Fatal("fixture mutable traversal absent")
	}
	workflow.Jobs["test"].Steps[0].Pos.Line = 999
	scopes[0].Context = invocation.Context{}
	placement.Directory.Path = "corrupt placement"
	scopes[0].Paths.Placements = workflowexecution.NewCheckoutPlacement(placement, workflowexecution.CheckoutPlacement{})
	clonedScopes, clonedUnits := batch.Snapshot()
	if clonedUnits[0].Scope != clonedScopes[0] || clonedUnits[1].Scope != clonedScopes[0] || clonedUnits[0].Content != nil || clonedUnits[1].Content != nil {
		t.Fatal("snapshot lost scope ownership or workflow source fallback")
	}
	clonedScopes[0].Context = invocation.Context{}
	placement = clonedScopes[0].Paths.Placements.Details()
	placement.Directory.Path = "corrupt returned placement"
	clonedScopes[0].Paths.Placements = workflowexecution.NewCheckoutPlacement(placement, workflowexecution.CheckoutPlacement{})
	clonedUnits[0].Owner.Step.Line = 888
	clonedUnits[0].Origin.StepPosition.Line = 777
	after, _ := batch.Fact(0, false)
	afterInput, _ := batch.Fact(1, false)
	if !reflect.DeepEqual(first, after) || !reflect.DeepEqual(input, afterInput) {
		t.Fatal("caller mutation changed a prepared fact")
	}
	_, fresh := batch.Snapshot()
	if fresh[0].Scope.Paths.Placements.Details().Directory.Path != "original" {
		t.Fatal("snapshot mutation changed checkout placement")
	}
	if fresh[0].Content != nil || fresh[0].Owner.Step.Line != 6 || fresh[0].Origin.StepPosition.Line != 6 || fresh[0].Scope.Context.ID() != graph.RootID() {
		t.Fatal("snapshot mutation changed source provenance")
	}
}

func assertBatchFactsMatchCanonicalDeclarations(t *testing.T, batch Batch, registry *documentparse.SourceSnapshots) {
	t.Helper()
	_, units := batch.Snapshot()
	for index, unit := range units {
		declaration, exists := registry.Declaration(unit.Reference)
		if !exists {
			t.Fatalf("canonical declaration %q is missing", unit.Reference)
		}
		entry, ok := registry.Lookup(batch.data.graph.Entry().Reference)
		if !ok {
			t.Fatal("entry snapshot is missing")
		}
		var cursor *workflowlang.Step
		if entry.Kind() == documentparse.KindWorkflow {
			parsed, ok := entry.Workflow()
			if !ok {
				t.Fatal("entry workflow is unavailable")
			}
			for _, job := range parsed.Jobs {
				for _, step := range job.Steps {
					reference, err := entry.StepReference(step)
					if err != nil {
						t.Fatal(err)
					}
					if reference == unit.Reference {
						cursor = step
					}
				}
			}
		} else {
			steps, ok := entry.ActionSteps()
			if !ok {
				t.Fatal("entry action is unavailable")
			}
			for _, step := range steps {
				reference, err := entry.StepReference(step)
				if err != nil {
					t.Fatal(err)
				}
				if reference == unit.Reference {
					cursor = step
				}
			}
		}
		if cursor == nil {
			t.Fatalf("legacy cursor for declaration %q is missing", unit.Reference)
		}
		cursorView, err := entry.StepView(cursor)
		if err != nil || cursorView != declaration {
			t.Fatalf("registry declaration is not its canonical cursor view: %v", err)
		}
		projected := unit
		if run, isRun := declaration.Run(); isRun {
			projected.Run, projected.Input = run, nil
		} else if input, isInput := declaration.Action(); isInput && unit.Input != nil {
			inputs, _ := input.InputNames()
			var found documentparse.InputView
			for name, candidate := range inputs {
				if name == unit.Input.Site.Input {
					found = candidate
					break
				}
			}
			projected.Run = documentparse.ExecRunView{}
			projected.Input = &ActionInput{Site: unit.Input.Site, Value: found}
		} else {
			t.Fatalf("canonical declaration kind disagrees with batch unit %d", index)
		}
		want := fact(projected, batch.data.graph, false)
		occurrence, found := batch.data.projection.Script(unit.Occurrence)
		if !found {
			t.Fatalf("script occurrence %q is missing from projection", unit.Occurrence)
		}
		want.Occurrence = occurrence
		got, exists := batch.Fact(index, false)
		if !exists || !reflect.DeepEqual(got, want) {
			t.Fatalf("fact %d differs from canonical declaration: got=%+v want=%+v", index, got, want)
		}
	}
}

func TestScopesOnlyBatchRetainsGraphWithoutMaterializedScripts(t *testing.T) {
	graph, collection := batchFixture(t, false)
	batch, err := NewBatch(graph, Collection{Scopes: collection.Scopes})
	if err != nil || !batch.Valid() || batch.CollectedScripts() || len(batch.Scripts()) != 0 || len(batch.Invocations().Scripts()) != 1 {
		t.Fatalf("scopes-only batch: %v", err)
	}
	if _, ok := batch.Fact(0, false); ok {
		t.Fatal("fabricated uncollected fact")
	}
}

func TestScopesOnlyBatchCannotDropCompletedScopes(t *testing.T) {
	graph, collection := batchFixture(t, false)
	scopes := collection.Scopes
	if _, err := NewBatch(graph, Collection{}); !errors.Is(err, ErrInvalidBatch) {
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
	if !admitted {
		t.Fatal("fixture call was not admitted")
	}
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
	if _, err := NewBatch(graph, Collection{Scopes: scopes}); !errors.Is(err, ErrInvalidBatch) {
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

func TestConcurrentBatchConsumersCannotMutatePublishedFacts(t *testing.T) {
	graph, collection := batchFixture(t, true)
	run, _ := fixtureBodies(t, graph, collection)
	run.Source.Comments = []workflowlang.ScalarComment{{Text: "forged", Pos: workflowlang.Pos{Line: 1, Col: 1}}}
	batch, err := NewBatch(graph, collection)
	if err != nil {
		t.Fatal(err)
	}
	for range 4 {
		t.Run("consumer", func(t *testing.T) {
			t.Parallel()
			for range 25 {
				fact, ok := batch.Fact(0, false)
				if !ok || fact.Text != "echo ok" || fact.Comments[0].Text != "# original" || fact.Spans[0].Document.Line == 999 {
					t.Fatal("shared mutable fact")
				}
				fact.Comments[0].Text = "changed"
				fact.Spans[0].Document.Line = 999
				_, snapshot := batch.Snapshot()
				snapshot[0].Run = documentparse.ExecRunView{}
				snapshot[0].Owner.Step.Line = 999
			}
		})
	}
}
