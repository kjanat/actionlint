package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/input/source"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestRunOneOwnsIsolationDefaultsAndPartialError(t *testing.T) {
	t.Parallel()
	failure := errors.New("incomplete")
	options := json.RawMessage(`{"mode":"safe"}`)
	input := checkinputs.Input{
		Path: "unit.yml", Options: json.RawMessage(`{"unregistered":true}`), AvailableInputs: []rulemeta.InputKind{rulemeta.Scripts},
		Mappings:     []workflowfacts.Mapping{{Scope: "original", Entries: []workflowfacts.Entry{{Name: "original"}}}},
		Dependencies: []workflowfacts.Dependency{{Uses: "original"}}, Files: []checkinputs.FileSystem{{Path: "/original", FS: fstest.MapFS{}}},
		SourceRoots: []checkinputs.SourceRoot{{Host: "/host", Guest: "/original"}},
		Scripts:     []checkinputs.Script{{Text: "echo ok", Spans: []scriptmodel.Span{{Line: 1, End: 8}}, Comments: []workflowfacts.SourceComment{{Text: "# original"}}, Runtime: &scriptmodel.Execution{Shell: "bash"}}},
	}
	registration := checkinputs.Registration{Options: options, Checker: testAnalyzer{
		descriptor: checkinputs.Descriptor{Descriptor: rulemeta.Descriptor{Name: "single", Explanation: "explain", Help: "help", DocumentationURL: "https://example.invalid/rule"}},
		run: func(_ context.Context, supplied checkinputs.Input) ([]results.Diagnostic, error) {
			if string(supplied.Options) != string(options) {
				t.Fatalf("input options replaced registered options: %s", supplied.Options)
			}
			supplied.Options[0] = '!'
			supplied.AvailableInputs[0] = rulemeta.Source
			supplied.Scripts[0].Spans[0].End = 99
			supplied.Scripts[0].Comments[0].Text = "changed"
			supplied.Scripts[0].Runtime.Shell = "changed"
			supplied.Scripts[0].Text = "changed"
			supplied.Mappings[0].Scope = "changed"
			supplied.Mappings[0].Entries[0].Name = "changed"
			supplied.Dependencies[0].Uses = "changed"
			supplied.Files[0] = checkinputs.FileSystem{Path: "/changed"}
			supplied.SourceRoots[0].Guest = "/changed"
			return []results.Diagnostic{{Message: "partial"}, {Rule: "explicit", Path: "foreign.sh", Explanation: "own", Help: "own", DocumentationURL: "https://example.invalid/own"}}, failure
		},
	}}
	findings, err := RunOne(t.Context(), input, prepareOne(t, registration, input.AvailableInputs))
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), `analyzer "single"`) || len(findings) != 2 {
		t.Fatalf("partial result changed: %v %v", findings, err)
	}
	if got := findings[0]; got.Path != input.Path || got.Rule != "single" || got.Explanation != "explain" || got.Help != "help" || got.DocumentationURL != "https://example.invalid/rule" {
		t.Fatalf("descriptor defaults missing: %+v", got)
	}
	if got := findings[1]; got.Path != "foreign.sh" || got.Rule != "explicit" || got.Explanation != "own" || got.Help != "own" || got.DocumentationURL != "https://example.invalid/own" {
		t.Fatalf("explicit diagnostic values overwritten: %+v", got)
	}
	if string(options) != `{"mode":"safe"}` || string(input.Options) != `{"unregistered":true}` || input.AvailableInputs[0] != rulemeta.Scripts || input.Scripts[0].Spans[0].End != 8 || input.Scripts[0].Comments[0].Text != "# original" || input.Scripts[0].Runtime.Shell != "bash" {
		t.Fatal("RunOne mutated parent input or registration")
	}
	if input.Scripts[0].Text != "echo ok" || input.Mappings[0].Scope != "original" || input.Mappings[0].Entries[0].Name != "original" || input.Dependencies[0].Uses != "original" || input.Files[0].Path != "/original" || input.Files[0].FS == nil || input.SourceRoots[0].Guest != "/original" {
		t.Fatal("RunOne shared mutable input collections")
	}
}

func TestForwardedAnalyzerUsesSharedAdmission(t *testing.T) {
	t.Parallel()
	failure := errors.New("runtime failure")
	input := checkinputs.Input{Path: "workflow.yml", AvailableInputs: []rulemeta.InputKind{rulemeta.Scripts}, Scripts: []checkinputs.Script{{Text: "original"}}}
	calls := 0
	prepared, err := PrepareForwarding(checkinputs.Registration{Checker: testAnalyzer{
		descriptor: checkinputs.Descriptor{Descriptor: rulemeta.Descriptor{Name: "forwarded", RequiredInputs: []rulemeta.InputKind{rulemeta.Scripts}}},
		run: func(_ context.Context, supplied checkinputs.Input) ([]results.Diagnostic, error) {
			calls++
			supplied.Scripts[0].Text = "changed"
			return []results.Diagnostic{{Message: "partial"}}, failure
		},
	}}, input.AvailableInputs)
	if err != nil {
		t.Fatal(err)
	}
	findings, err := RunOne(t.Context(), input, prepared)
	if !errors.Is(err, failure) || err.Error() != failure.Error() || len(findings) != 1 || findings[0].Rule != "forwarded" || input.Scripts[0].Text != "original" {
		t.Fatalf("forwarding lost error text, metadata or isolation: %v %v", findings, err)
	}
	input.AvailableInputs = append(input.AvailableInputs, rulemeta.Invocations)
	if _, err := RunOne(t.Context(), input, prepared); !errors.Is(err, checkinputs.ErrInvalidCallGraph) || calls != 1 {
		t.Fatalf("forwarding bypassed graph admission: calls=%d error=%v", calls, err)
	}
}

func TestRunInputValidationSurvivesNoRegistrations(t *testing.T) {
	t.Parallel()
	input := checkinputs.Input{AvailableInputs: []rulemeta.InputKind{rulemeta.Invocations}}
	if _, err := Run(t.Context(), input, nil); !errors.Is(err, checkinputs.ErrInvalidCallGraph) {
		t.Fatalf("empty registration list skipped input validation: %v", err)
	}
	calls := 0
	registration := checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rulemeta.Descriptor{Name: "never"}}, run: func(context.Context, checkinputs.Input) ([]results.Diagnostic, error) {
		calls++
		return nil, nil
	}}}
	if _, err := RunOne(t.Context(), input, prepareOne(t, registration, input.AvailableInputs)); !errors.Is(err, checkinputs.ErrInvalidCallGraph) || calls != 0 {
		t.Fatalf("RunOne skipped input validation: calls=%d error=%v", calls, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := RunOne(ctx, checkinputs.Input{}, prepareOne(t, registration, nil)); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("RunOne started canceled work: calls=%d error=%v", calls, err)
	}
}

type cancelingDescriptorAnalyzer struct {
	cancel context.CancelFunc
	calls  *int
}

func (a cancelingDescriptorAnalyzer) Descriptor() checkinputs.Descriptor {
	a.cancel()
	return checkinputs.Descriptor{Name: "canceled-before-analysis"}
}

func (a cancelingDescriptorAnalyzer) CheckDocument(context.Context, checkinputs.Input) ([]results.Diagnostic, error) {
	*a.calls++
	return nil, nil
}

func TestRunOneDoesNotStartAnalysisAfterPreparationCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	registration := checkinputs.Registration{Checker: cancelingDescriptorAnalyzer{cancel: cancel, calls: &calls}}
	if _, err := RunOne(ctx, checkinputs.Input{}, prepareOne(t, registration, nil)); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("canceled preparation started analyzer: calls=%d error=%v", calls, err)
	}
}

func prepareOne(t *testing.T, registration checkinputs.Registration, available []rulemeta.InputKind) PreparedRegistration {
	t.Helper()
	prepared, err := Prepare(registration, available)
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func TestPrepareValidatesRegistration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		registration checkinputs.Registration
		available    []rulemeta.InputKind
		want         string
	}{
		{name: "nil analyzer", want: "nil analyzer registration"},
		{name: "typed nil analyzer", registration: checkinputs.Registration{Checker: (*testAnalyzer)(nil)}, want: "nil analyzer registration"},
		{name: "empty name", registration: checkinputs.Registration{Checker: testAnalyzer{}}, want: "name must not be empty"},
		{name: "unsupported requirement", registration: checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rulemeta.Descriptor{Name: "future", RequiredInputs: []rulemeta.InputKind{"future"}}}}}, want: `requires unsupported input kind "future"`},
		{name: "unavailable requirement", registration: checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rulemeta.Descriptor{Name: "source", RequiredInputs: []rulemeta.InputKind{rulemeta.Source}}}}}, want: `requires unavailable input kind "source"`},
		{name: "invalid options", registration: checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rulemeta.Descriptor{Name: "options"}}}, Options: json.RawMessage("{")}, want: `analyzer "options" options:`},
		{name: "schema mismatch", registration: checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rulemeta.Descriptor{Name: "options", OptionsSchema: json.RawMessage(`{"required":["mode"]}`)}}}, Options: json.RawMessage(`{}`)}, want: `analyzer "options" options:`},
		{name: "invalid schema", registration: checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rulemeta.Descriptor{Name: "options", OptionsSchema: json.RawMessage("{")}}}}, want: `analyzer "options" options:`},
		{name: "unsupported available", registration: checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rulemeta.Descriptor{Name: "facts"}}}}, available: []rulemeta.InputKind{"future"}, want: "unsupported available input kind"},
		{name: "duplicate available", registration: checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rulemeta.Descriptor{Name: "facts"}}}}, available: []rulemeta.InputKind{rulemeta.Source, rulemeta.Source}, want: "duplicate available input kind"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Prepare(tc.registration, tc.available); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Prepare error=%v, want %q", err, tc.want)
			}
		})
	}
}

func TestRunRejectsUnsupportedAndDuplicateAvailableFactsPerRegistration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		available []rulemeta.InputKind
		want      string
	}{
		{name: "unsupported", available: []rulemeta.InputKind{"future"}, want: `unsupported available input kind "future"`},
		{name: "duplicate", available: []rulemeta.InputKind{rulemeta.Source, rulemeta.Source}, want: `duplicate available input kind "source"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			registration := checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rulemeta.Descriptor{Name: "never"}}, run: func(context.Context, checkinputs.Input) ([]results.Diagnostic, error) {
				calls.Add(1)
				return nil, nil
			}}}
			findings, err := Run(t.Context(), checkinputs.Input{AvailableInputs: tc.available}, []checkinputs.Registration{registration, registration})
			if len(findings) != 0 || calls.Load() != 0 || err == nil || strings.Count(err.Error(), tc.want) != 2 {
				t.Fatalf("invalid input kinds did not surface per registration: calls=%d findings=%v error=%v", calls.Load(), findings, err)
			}
		})
	}
}

type descriptorCountingAnalyzer struct {
	descriptor checkinputs.Descriptor
	calls      int
	run        func(context.Context, checkinputs.Input) ([]results.Diagnostic, error)
}

func (a *descriptorCountingAnalyzer) Descriptor() checkinputs.Descriptor {
	a.calls++
	return a.descriptor
}

func (a *descriptorCountingAnalyzer) CheckDocument(ctx context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	return a.run(ctx, input)
}

func TestPreparedRegistrationOwnsMetadataAndRequiresExactFacts(t *testing.T) {
	t.Parallel()
	options := json.RawMessage(`{"mode":"safe"}`)
	available := []rulemeta.InputKind{rulemeta.Source, rulemeta.Mappings}
	calls := 0
	a := &descriptorCountingAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rulemeta.Descriptor{Name: "original", OptionsSchema: json.RawMessage(`{"type":"object","required":["mode"]}`), RequiredInputs: []rulemeta.InputKind{rulemeta.Source}}}, run: func(_ context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
		calls++
		if string(input.Options) != `{"mode":"safe"}` {
			t.Fatalf("prepared options changed: %s", input.Options)
		}
		input.Options[0] = '!'
		return []results.Diagnostic{{Message: "ok"}}, nil
	}}
	prepared := prepareOne(t, checkinputs.Registration{Checker: a, Options: options}, available)
	options[0] = '!'
	available[0] = rulemeta.Execution
	a.descriptor.Name = "changed"
	a.descriptor.RequiredInputs[0] = "future"
	a.descriptor.OptionsSchema[0] = '!'
	for range 2 {
		findings, err := RunOne(t.Context(), checkinputs.Input{AvailableInputs: []rulemeta.InputKind{rulemeta.Mappings, rulemeta.Source}}, prepared)
		if err != nil || len(findings) != 1 || findings[0].Rule != "original" {
			t.Fatalf("prepared metadata changed: %v %v", findings, err)
		}
	}
	if a.calls != 1 || calls != 2 {
		t.Fatalf("descriptor validated repeatedly: descriptor=%d analyze=%d", a.calls, calls)
	}
	for _, facts := range [][]rulemeta.InputKind{nil, {rulemeta.Source}, {rulemeta.Source, rulemeta.Mappings, rulemeta.Dependencies}, {rulemeta.Source, rulemeta.Source, rulemeta.Mappings}} {
		if _, err := RunOne(t.Context(), checkinputs.Input{AvailableInputs: facts}, prepared); err == nil {
			t.Fatalf("prepared registration accepted mismatched input kinds: %v", facts)
		}
	}
	if _, err := RunOne(t.Context(), checkinputs.Input{}, PreparedRegistration{}); err == nil || !strings.Contains(err.Error(), "unprepared") {
		t.Fatalf("zero prepared registration accepted: %v", err)
	}
	if calls != 2 {
		t.Fatalf("invalid prepared input executed: %d calls", calls)
	}
}

func TestRunOneClonesInvocationAndActionInputFacts(t *testing.T) {
	t.Parallel()
	root, callID := checkinputs.OccurrenceID(strings.Repeat("1", 64)), checkinputs.OccurrenceID(strings.Repeat("2", 64))
	runID, inputID := checkinputs.OccurrenceID(strings.Repeat("3", 64)), checkinputs.OccurrenceID(strings.Repeat("4", 64))
	entry := source.Reference{Document: "workflow", Revision: "r", Node: "document"}
	target := source.Reference{Document: "action", Revision: "r", Node: "document"}
	outcome, err := checkinputs.NewCallOutcome(checkinputs.ExpandedCall{Target: target})
	if err != nil {
		t.Fatal(err)
	}
	occurrences := []checkinputs.ScriptOccurrence{
		{ID: runID, Owner: callID, Ancestry: []checkinputs.OccurrenceID{root, callID}, Depth: 1, Declaration: source.Reference{Document: "action", Revision: "r", Node: "run"}, Kind: checkinputs.RunScript},
		{ID: inputID, Owner: callID, Ancestry: []checkinputs.OccurrenceID{root, callID}, Depth: 1, Declaration: source.Reference{Document: "action", Revision: "r", Node: "input"}, Kind: checkinputs.ActionInputScript, Input: "script"},
	}
	graph, err := checkinputs.NewCallGraph(checkinputs.CallGraphData{
		Entry:  checkinputs.InvocationEntry{ID: root, Kind: checkinputs.WorkflowEntry, Reference: entry},
		Budget: checkinputs.InvocationBudget{MaxDepth: 1, MaxCalls: 1, MaxScripts: 2}, Complete: true,
		Calls: []checkinputs.Call{{ID: callID, Parent: root, Ancestry: []checkinputs.OccurrenceID{root}, Depth: 1, Site: source.Reference{Document: "workflow", Revision: "r", Node: "call"}, Condition: checkinputs.CallEnabled, Outcome: outcome}}, Scripts: occurrences,
	})
	if err != nil {
		t.Fatal(err)
	}
	input := checkinputs.Input{
		Reference: entry, CallGraph: graph, AvailableInputs: []rulemeta.InputKind{rulemeta.Scripts, rulemeta.Invocations}, SelectedScripts: []checkinputs.OccurrenceID{runID, inputID},
		Invoked:      []checkinputs.Script{{Reference: occurrences[0].Declaration, Text: "original", Spans: []scriptmodel.Span{{End: 8}}, Comments: []workflowfacts.SourceComment{{Text: "original"}}, Occurrence: occurrences[0], Runtime: &scriptmodel.Execution{Shell: "bash"}}},
		ActionInputs: []checkinputs.ActionInput{{Reference: occurrences[1].Declaration, Text: "original", Spans: []scriptmodel.Span{{End: 8}}, Comments: []workflowfacts.SourceComment{{Text: "original"}}, Occurrence: occurrences[1], Site: checkinputs.ScriptInput{Action: "owner/action", Input: "script", Language: "javascript"}}},
	}
	want := graph.Export()
	prepared := prepareOne(t, checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rulemeta.Descriptor{Name: "mutator"}}, run: func(_ context.Context, supplied checkinputs.Input) ([]results.Diagnostic, error) {
		supplied.SelectedScripts[0] = "changed"
		supplied.Invoked[0].Text = "changed"
		supplied.Invoked[0].Occurrence.Ancestry[0] = "changed"
		supplied.Invoked[0].Spans[0].End = 99
		supplied.Invoked[0].Comments[0].Text = "changed"
		supplied.Invoked[0].Runtime.Shell = "changed"
		supplied.ActionInputs[0].Text = "changed"
		supplied.ActionInputs[0].Occurrence.Ancestry[0] = "changed"
		supplied.ActionInputs[0].Spans[0].End = 99
		supplied.ActionInputs[0].Comments[0].Text = "changed"
		supplied.ActionInputs[0].Site.Language = "changed"
		return nil, nil
	}}}, input.AvailableInputs)
	if _, err := RunOne(t.Context(), input, prepared); err != nil {
		t.Fatal(err)
	}
	if input.SelectedScripts[0] != runID || input.Invoked[0].Text != "original" || input.Invoked[0].Occurrence.Ancestry[0] != root || input.Invoked[0].Spans[0].End != 8 || input.Invoked[0].Comments[0].Text != "original" || input.Invoked[0].Runtime.Shell != "bash" {
		t.Fatal("RunOne mutated invoked script or selection")
	}
	if input.ActionInputs[0].Text != "original" || input.ActionInputs[0].Occurrence.Ancestry[0] != root || input.ActionInputs[0].Spans[0].End != 8 || input.ActionInputs[0].Comments[0].Text != "original" || input.ActionInputs[0].Site.Language != "javascript" || !reflect.DeepEqual(graph.Export(), want) {
		t.Fatal("RunOne mutated action input or graph")
	}
}

func TestRunOneClonesSharedReturnedFindings(t *testing.T) {
	t.Parallel()
	shared := []results.Diagnostic{{Message: "cached"}}
	prepared := prepareOne(t, checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rulemeta.Descriptor{Name: "shared", Explanation: "why", Help: "help", DocumentationURL: "https://example.invalid"}}, run: func(context.Context, checkinputs.Input) ([]results.Diagnostic, error) { return shared, nil }}}, nil)
	var group sync.WaitGroup
	start := make(chan struct{})
	for i := range 16 {
		group.Go(func() {
			<-start
			path := fmt.Sprintf("%d.yml", i)
			findings, err := RunOne(t.Context(), checkinputs.Input{Path: path}, prepared)
			if err != nil || len(findings) != 1 || findings[0].Path != path || findings[0].Rule != "shared" {
				t.Errorf("shared output corrupted: %v %v", findings, err)
			}
		})
	}
	close(start)
	group.Wait()
	if !reflect.DeepEqual(shared, []results.Diagnostic{{Message: "cached"}}) {
		t.Fatalf("RunOne mutated analyzer-owned findings: %+v", shared)
	}
}

func TestRunOneRetainsCancellationAndPartialFindings(t *testing.T) {
	t.Parallel()
	for _, analyzerErr := range []error{nil, errors.New("partial failure"), fmt.Errorf("wrapped: %w", context.Canceled)} {
		t.Run(fmt.Sprint(analyzerErr), func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			prepared := prepareOne(t, checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rulemeta.Descriptor{Name: "canceling"}}, run: func(context.Context, checkinputs.Input) ([]results.Diagnostic, error) {
				cancel()
				return []results.Diagnostic{{Message: "partial"}}, analyzerErr
			}}}, nil)
			findings, err := RunOne(ctx, checkinputs.Input{}, prepared)
			if len(findings) != 1 || !errors.Is(err, context.Canceled) || analyzerErr != nil && !errors.Is(err, analyzerErr) || strings.Count(err.Error(), context.Canceled.Error()) != 1 {
				t.Fatalf("cancellation lost or duplicated: %v %v", findings, err)
			}
		})
	}
}

func TestRunOneLeavesAnalyzerPanicsToCaller(t *testing.T) {
	t.Parallel()
	prepared := prepareOne(t, checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rulemeta.Descriptor{Name: "panicking"}}, run: func(context.Context, checkinputs.Input) ([]results.Diagnostic, error) {
		panic("analyzer bug")
	}}}, nil)
	defer func() {
		if got := recover(); got != "analyzer bug" {
			t.Errorf("analyzer panic changed: %v", got)
		}
	}()
	_, _ = RunOne(t.Context(), checkinputs.Input{}, prepared)
	t.Fatal("analyzer panic was swallowed")
}
