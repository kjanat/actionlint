package execution

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/documentparse"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	"actionlint.kjanat.dev/v2/source"
)

func TestCallPositionAdmission(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pos     *workflowlang.Pos
		want    invocation.Position
		invalid bool
	}{
		{name: "absent"},
		{name: "unknown", pos: &workflowlang.Pos{}},
		{name: "known", pos: &workflowlang.Pos{Line: 2, Col: 3}, want: invocation.Position{Line: 2, Column: 3}},
		{name: "partial line", pos: &workflowlang.Pos{Line: 2}, invalid: true},
		{name: "partial column", pos: &workflowlang.Pos{Col: 3}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			context, err := testCallContext(t, &workflowlang.Step{Pos: tc.pos, Exec: &workflowlang.ExecAction{}}, runnerlang.MacOrLinux)
			if tc.invalid {
				if !errors.Is(err, invocation.ErrInvalidFacts) {
					t.Fatalf("malformed position admitted: %v", err)
				}
				return
			}
			facts, exists := context.Facts()
			if err != nil || !exists || facts.Step != tc.want {
				t.Fatalf("position round trip: %+v error=%v", facts.Step, err)
			}
		})
	}
}

func TestMetadataCallPositionsReachInvocationOwner(t *testing.T) {
	for _, body := range []string{
		"    - uses: ./child\n",
		"    - &call {uses: ./child}\n    - *call\n",
		"    - not-a-mapping\n",
		"    - uses: ./child\n      run: echo invalid\n",
	} {
		t.Run(body, func(t *testing.T) {
			parsed := documentparse.Action("action.yml", "action.yml", []byte("name: parent\ndescription: parent\nruns:\n  using: composite\n  steps:\n"+body))
			if len(parsed.Findings) != 0 || parsed.Facts() == nil {
				t.Fatalf("action parse failed: %v", parsed.Findings)
			}
			steps := parsed.Facts().Jobs[""].Steps
			if len(steps) == 0 {
				t.Fatal("fixture produced no steps")
			}
			for _, step := range steps {
				context, err := testCallContext(t, step, runnerlang.MacOrLinux)
				facts, exists := context.Facts()
				if err != nil || !exists || facts.Step.Line < 1 || facts.Step.Column < 1 || facts.Step.Line != step.Pos.Line || facts.Step.Column != step.Pos.Col {
					t.Fatalf("metadata call position: %+v error=%v", facts.Step, err)
				}
			}
		})
	}
}

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

func TestCallFactsPreserveUnicodeSourceColumns(t *testing.T) {
	line := "jobs: {é: {runs-on: ubuntu-latest, steps: [{uses: ./child}]}}"
	parsed := documentparse.Workflow("ci.yml", []byte("on: push\n"+line+"\n"))
	if len(parsed.Findings) != 0 {
		t.Fatal(parsed.Findings)
	}
	facts, err := CallFacts(parsed.Workflow.Jobs["é"].Steps[0], runnerlang.MacOrLinux)
	prefix, _, found := strings.Cut(line, "{uses")
	if !found {
		t.Fatal("fixture has no call mapping")
	}
	wantColumn := utf8.RuneCountInString(prefix) + 1
	if err != nil || facts.Step.Line != 2 || facts.Step.Column != wantColumn {
		t.Fatalf("call coordinate=%+v want 2:%d error=%v", facts.Step, wantColumn, err)
	}
}

func testEntryContext(t *testing.T) invocation.Context {
	t.Helper()
	builder, err := invocation.NewBuilder(invocation.EntryAction, source.Reference{Document: "fixture", Node: "entry"}, invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	return builder.RootContext()
}
func testCallContext(t *testing.T, step *workflowlang.Step, platform runnerlang.Platform) (invocation.Context, error) {
	t.Helper()
	facts, err := CallFacts(step, platform)
	if err != nil {
		return invocation.Context{}, err
	}
	builder, err := invocation.NewBuilder(invocation.EntryWorkflow, source.Reference{Document: "fixture", Node: "entry"}, invocation.DefaultBudget())
	if err != nil {
		return invocation.Context{}, err
	}
	ticket, err := builder.Begin(builder.RootContext(), source.Reference{Document: "fixture", Node: "call"}, facts)
	if err != nil {
		return invocation.Context{}, err
	}
	context, admitted := ticket.Context()
	if !admitted {
		t.Fatal("fixture call was not admitted")
	}
	return context, nil
}
