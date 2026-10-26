package execution

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/documentparse"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
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
			position := invocation.Position{}
			if tc.pos != nil {
				position = invocation.Position{Line: tc.pos.Line, Column: tc.pos.Col}
			}
			facts := invocation.Facts{HasUses: true, Uses: "$/action", Step: position, Condition: invocation.ConditionEnabled}
			builder, err := invocation.NewBuilder(invocation.EntryWorkflow, source.Reference{Document: "fixture", Node: "entry"}, invocation.DefaultBudget())
			if err != nil {
				t.Fatal(err)
			}
			ticket, err := builder.Begin(builder.RootContext(), source.Reference{Document: "fixture", Node: "call"}, facts)
			if tc.invalid {
				if !errors.Is(err, invocation.ErrInvalidFacts) {
					t.Fatalf("malformed position admitted: %v", err)
				}
				return
			}
			context, admitted := ticket.Context()
			got, exists := context.Facts()
			if err != nil || !admitted || !exists || got.Step != tc.want {
				t.Fatalf("position round trip: %+v error=%v", got.Step, err)
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
			var snapshots documentparse.SourceSnapshots
			snapshot, err := snapshots.Action("action.yml", parsed.Metadata)
			if err != nil {
				t.Fatal(err)
			}
			steps, _ = snapshot.ActionSteps()
			for _, step := range steps {
				if _, ok := step.Exec.(*workflowlang.ExecAction); !ok {
					continue
				}
				view, err := snapshot.StepView(step)
				if err != nil {
					t.Fatal(err)
				}
				facts, err := CallFacts(view, runnerlang.MacOrLinux)
				if err != nil || facts.Step.Line < 1 || facts.Step.Column < 1 || facts.Step.Line != step.Pos.Line || facts.Step.Column != step.Pos.Col {
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
		if !ok || effects.Condition != tc.want || effects.Step.Line != 6 || effects.Step.Column != 9 || effects.Uses != "$/action" {
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
	child := parent.ForkComposite(testEntryContext(t), "action-source", Paths{ActionRunnerPath: "stale", ActionIndependent: true}, nil)
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
		facts := invocation.Facts{Condition: invocation.ConditionEnabled, HasUses: present}
		if present {
			facts.Uses = ""
		}
		context := testFactsContext(t, facts)
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
	var snapshots documentparse.SourceSnapshots
	snapshot, err := snapshots.Document("ci.yml", []byte("on: push\n"+line+"\n"), parsed)
	if err != nil {
		t.Fatal(err)
	}
	view, err := snapshot.StepView(parsed.Workflow.Jobs["é"].Steps[0])
	if err != nil {
		t.Fatal(err)
	}
	facts, err := CallFacts(view, runnerlang.MacOrLinux)
	prefix, _, found := strings.Cut(line, "{uses")
	if !found {
		t.Fatal("fixture has no call mapping")
	}
	wantColumn := utf8.RuneCountInString(prefix) + 1
	if err != nil || facts.Step.Line != 2 || facts.Step.Column != wantColumn {
		t.Fatalf("call coordinate=%+v want 2:%d error=%v", facts.Step, wantColumn, err)
	}
}

func TestCanonicalActionInputsPreserveSourceScalarTags(t *testing.T) {
	content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n        with:\n          null-value: null\n          null-text: 'null'\n")
	parsed := documentparse.Workflow("workflow.yml", content)
	if len(parsed.Findings) != 0 {
		t.Fatal(parsed.Findings)
	}
	var snapshots documentparse.SourceSnapshots
	snapshot, err := snapshots.Document("workflow.yml", content, parsed)
	if err != nil {
		t.Fatal(err)
	}
	step, err := snapshot.StepView(parsed.Workflow.Jobs["test"].Steps[0])
	if err != nil {
		t.Fatal(err)
	}
	action, present := step.Action()
	if !present {
		t.Fatal("action view unavailable")
	}
	inputs := action.ResolvedInputs()
	if got := inputs.Lookup("null-value"); !got.Known() || got.Kind() != value.Null {
		t.Fatalf("canonical unquoted null state=%v kind=%v", got.State(), got.Kind())
	}
	if got := inputs.Lookup("null-text"); !got.Known() || got.Kind() != value.String {
		t.Fatalf("canonical quoted null state=%v kind=%v", got.State(), got.Kind())
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
	action, ok := step.Exec.(*workflowlang.ExecAction)
	if !ok {
		return invocation.Context{}, ErrInvalidActionCall
	}
	uses := "$/fixture"
	if action.Uses != nil {
		uses = action.Uses.Value
	}
	var content strings.Builder
	content.WriteString("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ")
	content.WriteString(strconv.Quote(uses))
	if step.If != nil {
		content.WriteString("\n        if: " + strconv.Quote(step.If.Value))
	}
	if step.ContinueOnError != nil {
		content.WriteString("\n        continue-on-error: " + strconv.FormatBool(step.ContinueOnError.Value))
	}
	if step.Background != nil {
		content.WriteString("\n        background: " + strconv.FormatBool(step.Background.Value))
	}
	content.WriteByte('\n')
	bytes := []byte(content.String())
	parsed := documentparse.Workflow("workflow.yml", bytes)
	if len(parsed.Findings) != 0 {
		return invocation.Context{}, errors.New("call context fixture did not parse")
	}
	var snapshots documentparse.SourceSnapshots
	snapshot, err := snapshots.Document("workflow.yml", bytes, parsed)
	if err != nil {
		return invocation.Context{}, err
	}
	view, err := snapshot.StepView(parsed.Workflow.Jobs["test"].Steps[0])
	if err != nil {
		return invocation.Context{}, err
	}
	facts, err := CallFacts(view, platform)
	if err != nil {
		return invocation.Context{}, err
	}
	return testFactsContext(t, facts), nil
}

func testFactsContext(t *testing.T, facts invocation.Facts) invocation.Context {
	t.Helper()
	builder, err := invocation.NewBuilder(invocation.EntryWorkflow, source.Reference{Document: "fixture", Node: "entry"}, invocation.DefaultBudget())
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := builder.Begin(builder.RootContext(), source.Reference{Document: "fixture", Node: "call"}, facts)
	if err != nil {
		t.Fatal(err)
	}
	context, admitted := ticket.Context()
	if !admitted {
		t.Fatal("fixture call was not admitted")
	}
	return context
}
