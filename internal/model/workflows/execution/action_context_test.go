package execution

import (
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestCallPositionAdmission(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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
	t.Parallel()
	for _, body := range []string{
		"    - uses: ./child\n",
		"    - &call {uses: ./child}\n    - *call\n",
		"    - not-a-mapping\n",
		"    - uses: ./child\n      run: echo invalid\n",
	} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			parsed := parsing.Action("action.yml", []byte("name: parent\ndescription: parent\nruns:\n  using: composite\n  steps:\n"+body))
			if len(parsed.SyntaxErrors) != 0 || parsed.Facts == nil {
				t.Fatalf("action parse failed: %v", parsed.SyntaxErrors)
			}
			steps := parsed.Facts.Jobs[""].Steps
			if len(steps) == 0 {
				t.Fatal("fixture produced no steps")
			}
			snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
			snapshot, err := snapshots.Action("action.yml", parsed.Metadata)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := snapshot.ActionView()
			for step := range body.Steps() {
				if _, ok := step.Action(); !ok {
					continue
				}
				position, present := step.Pos()
				facts, err := CallFacts(step, runnerlang.MacOrLinux)
				if err != nil || !present || facts.Step.Line < 1 || facts.Step.Column < 1 || facts.Step.Line != position.Line || facts.Step.Column != position.Col {
					t.Fatalf("metadata call position: %+v error=%v", facts.Step, err)
				}
			}
		})
	}
}

func TestActionContextSnapshotsCallerEffects(t *testing.T) {
	t.Parallel()
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
		storedFacts, _ := context.Facts()
		if storedFacts.Condition != tc.want {
			t.Fatal("caller mutated stored effects")
		}
	}
	if _, exists := testEntryContext(t).Facts(); exists {
		t.Fatal("entry fabricated caller effects")
	}
}

func TestEmptyUsesAndAbsentUsesRetainDistinctOriginSemantics(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, present := range []bool{false, true} {
		facts := invocation.Facts{Condition: invocation.ConditionEnabled, HasUses: present}
		if present {
			facts.Uses = ""
		}
		context := testFactsContext(t, facts)
		paths := Paths{Workspace: root, Placements: NewCheckoutPlacement(Checkout{Directory: Directory{Kind: DirectoryKnown}, CaseInsensitive: true}, CheckoutPlacement{})}
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
	t.Parallel()
	line := "jobs: {é: {runs-on: ubuntu-latest, steps: [{uses: ./child}]}}"
	parsed := parsing.Workflow([]byte("on: push\n" + line + "\n"))
	if len(parsed.SyntaxErrors) != 0 {
		t.Fatal(parsed.SyntaxErrors)
	}
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document("ci.yml", []byte("on: push\n"+line+"\n"), parsed)
	if err != nil {
		t.Fatal(err)
	}
	workflow, _ := snapshot.WorkflowView()
	job, _ := workflow.Job("é")
	view := slices.Collect(job.Steps())[0]
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
	t.Parallel()
	content := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n        with:\n          null-value: null\n          null-text: 'null'\n")
	parsed := parsing.Workflow(content)
	if len(parsed.SyntaxErrors) != 0 {
		t.Fatal(parsed.SyntaxErrors)
	}
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document("workflow.yml", content, parsed)
	if err != nil {
		t.Fatal(err)
	}
	workflow, _ := snapshot.WorkflowView()
	job, _ := workflow.Job("test")
	step := slices.Collect(job.Steps())[0]
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
		content.WriteString("\n        if: ")
		content.WriteString(strconv.Quote(step.If.Value))
	}
	if step.ContinueOnError != nil {
		content.WriteString("\n        continue-on-error: ")
		content.WriteString(strconv.FormatBool(step.ContinueOnError.Value))
	}
	if step.Background != nil {
		content.WriteString("\n        background: ")
		content.WriteString(strconv.FormatBool(step.Background.Value))
	}
	content.WriteByte('\n')
	bytes := []byte(content.String())
	parsed := parsing.Workflow(bytes)
	if len(parsed.SyntaxErrors) != 0 {
		return invocation.Context{}, errors.New("call context fixture did not parse")
	}
	snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document("workflow.yml", bytes, parsed)
	if err != nil {
		return invocation.Context{}, err
	}
	workflow, _ := snapshot.WorkflowView()
	job, _ := workflow.Job("test")
	view := slices.Collect(job.Steps())[0]
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
