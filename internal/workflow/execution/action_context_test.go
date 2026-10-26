package execution

import (
	"path/filepath"
	"testing"
	"actionlint.kjanat.dev/v2/source"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
)

func TestActionContextSnapshotsCallerEffects(t *testing.T) {
	for _, tc := range []struct {
		condition *workflowlang.String
		want      invocation.Condition
	}{
		{nil, invocation.ConditionEnabled},
		{&workflowlang.String{Value: "false"}, invocation.ConditionSkipped},
		{&workflowlang.String{Value: "github.event.repository.name"}, invocation.ConditionRuntimeDependent},
	} {
		call := &workflowlang.Step{Pos: &workflowlang.Pos{Line: 4, Col: 3}, If: tc.condition, Exec: &workflowlang.ExecAction{Uses: &workflowlang.String{Value: "$/action"}}}
		context, err := testCallContext(t, call, runnerlang.MacOrLinux)
		if err != nil {
			t.Fatal(err)
		}
		call.Pos.Line = 99
		effects, ok := context.Facts()
		if !ok || effects.Condition != tc.want || effects.Step.Line != 4 || effects.Uses != "$/action" {
			t.Fatalf("call effects: %+v", effects)
		}
		effects.Condition = invocation.ConditionInvalid
		retained, _ := context.Facts()
		if retained.Condition != tc.want {
			t.Fatal("caller mutated stored effects")
		}
	}
	if _, exists := testEntryContext(t).Facts(); exists {
		t.Fatal("entry fabricated caller effects")
	}
}

func TestActionEntryInitializesPathsAndPreservesCheckerFlags(t *testing.T) {
	parent := NewExecutableChecker(ExecutableContext{})
	parent.jobEnv, parent.jobGitEnv, parent.jobPathUnknown, parent.skipFindings = true, true, true, true
	child := parent.ForkComposite(testEntryContext(t), "action-source", Paths{ActionRunnerPath: "stale", ActionIndependent: true})
	if child.paths.ActionPath != "action-source" || child.paths.ActionRunnerPath != "" || child.paths.ActionIndependent {
		t.Fatalf("entry inherited caller origin: %+v", child.paths)
	}
	if !child.jobEnv || !child.jobGitEnv || !child.jobPathUnknown || !child.skipFindings {
		t.Fatal("entry lost prepared checker flags")
	}
}

func TestEmptyUsesAndAbsentUsesRetainDistinctOriginSemantics(t *testing.T) {
	root := t.TempDir()
	for _, present := range []bool{false, true} {
		action := &workflowlang.ExecAction{}
		if present {
			action.Uses = &workflowlang.String{Value: ""}
		}
		context, err := testCallContext(t, &workflowlang.Step{Exec: action}, runnerlang.MacOrLinux)
		if err != nil {
			t.Fatal(err)
		}
		paths := Paths{Workspace: root, Placements: &CheckoutPlacement{Directory: Directory{Kind: DirectoryKnown}, CaseInsensitive: true}}
		ActionOrigin(&paths, context, filepath.Join(root, "local", "action"))
		want := ""
		if present {
			want = "local/action"
		}
		if paths.ActionRunnerPath != want {
			t.Fatalf("present=%v: runner path %q, want %q", present, paths.ActionRunnerPath, want)
		}
	}
}

func testEntryContext(t *testing.T) invocation.Context {
	t.Helper()
	builder, err := invocation.NewBuilder(invocation.EntryAction, source.Reference{Document: "fixture", Node: "entry"}, invocation.DefaultBudget())
	if err != nil { t.Fatal(err) }
	return builder.RootContext()
}
func testCallContext(t *testing.T, step *workflowlang.Step, platform runnerlang.Platform) (invocation.Context, error) {
	t.Helper()
	facts, err := CallFacts(step, platform)
	if err != nil { return invocation.Context{}, err }
	builder, err := invocation.NewBuilder(invocation.EntryWorkflow, source.Reference{Document: "fixture", Node: "entry"}, invocation.DefaultBudget())
	if err != nil { return invocation.Context{}, err }
	ticket, err := builder.Begin(builder.RootContext(), source.Reference{Document: "fixture", Node: "call"}, facts)
	if err != nil { return invocation.Context{}, err }
	context, admitted := ticket.Context()
	if !admitted { t.Fatal("fixture call was not admitted") }
	return context, nil
}
