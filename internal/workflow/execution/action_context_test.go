package execution

import (
	"path/filepath"
	"testing"

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
		context, err := ActionCall(call, runnerlang.MacOrLinux)
		if err != nil {
			t.Fatal(err)
		}
		call.Pos.Line = 99
		effects, ok := context.Effects()
		if !ok || effects.Condition != tc.want || effects.Step.Line != 4 || effects.Uses != "$/action" {
			t.Fatalf("call effects: %+v", effects)
		}
		effects.Condition = invocation.ConditionInvalid
		retained, _ := context.Effects()
		if retained.Condition != tc.want {
			t.Fatal("caller mutated stored effects")
		}
	}
	if _, exists := ActionEntry().Effects(); exists {
		t.Fatal("entry fabricated caller effects")
	}
}

func TestActionEntryInitializesPathsAndPreservesCheckerFlags(t *testing.T) {
	parent := NewExecutableChecker(ExecutableContext{})
	parent.jobEnv, parent.jobGitEnv, parent.jobPathUnknown, parent.skipFindings = true, true, true, true
	child := parent.ForkComposite(ActionEntry(), "action-source", Paths{ActionRunnerPath: "stale", ActionIndependent: true})
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
		context, err := ActionCall(&workflowlang.Step{Exec: action}, runnerlang.MacOrLinux)
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
