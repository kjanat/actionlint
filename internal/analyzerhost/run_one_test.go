package analyzerhost

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

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/source"
)

func TestRunOneOwnsIsolationDefaultsAndPartialError(t *testing.T) {
	failure := errors.New("incomplete")
	options := json.RawMessage(`{"mode":"safe"}`)
	input := analyzer.Input{
		Path: "unit.yml", Options: json.RawMessage(`{"unregistered":true}`), Available: []analyzer.Fact{analyzer.Scripts},
		Mappings:     []analyzer.Mapping{{Scope: "original", Entries: []analyzer.Entry{{Name: "original"}}}},
		Dependencies: []analyzer.Dependency{{Uses: "original"}}, Files: []analyzer.FileSystem{{Path: "/original", FS: fstest.MapFS{}}},
		SourceRoots: []analyzer.SourceRoot{{Host: "/host", Guest: "/original"}},
		Scripts:     []analyzer.Script{{Text: "echo ok", Spans: []analyzer.Span{{Line: 1, End: 8}}, Comments: []analyzer.SourceComment{{Text: "# original"}}, Runtime: &analyzer.Runtime{Shell: "bash"}}},
	}
	registration := analyzer.Registration{Options: options, Analyzer: testAnalyzer{
		descriptor: analyzer.Descriptor{Name: "single", Explanation: "explain", Help: "help", DocumentationURL: "https://example.invalid/rule"},
		run: func(_ context.Context, supplied analyzer.Input) ([]diagnostic.Diagnostic, error) {
			if string(supplied.Options) != string(options) {
				t.Fatalf("input options replaced registered options: %s", supplied.Options)
			}
			supplied.Options[0] = '!'
			supplied.Available[0] = analyzer.Source
			supplied.Scripts[0].Spans[0].End = 99
			supplied.Scripts[0].Comments[0].Text = "changed"
			supplied.Scripts[0].Runtime.Shell = "changed"
			supplied.Scripts[0].Text = "changed"
			supplied.Mappings[0].Scope = "changed"
			supplied.Mappings[0].Entries[0].Name = "changed"
			supplied.Dependencies[0].Uses = "changed"
			supplied.Files[0] = analyzer.FileSystem{Path: "/changed"}
			supplied.SourceRoots[0].Guest = "/changed"
			return []diagnostic.Diagnostic{{Message: "partial"}, {Rule: "explicit", Path: "foreign.sh", Explanation: "own", Help: "own", DocumentationURL: "https://example.invalid/own"}}, failure
		},
	}}
	findings, err := RunOne(t.Context(), input, prepareOne(t, registration, input.Available))
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), `analyzer "single"`) || len(findings) != 2 {
		t.Fatalf("partial result changed: %v %v", findings, err)
	}
	if got := findings[0]; got.Path != input.Path || got.Rule != "single" || got.Explanation != "explain" || got.Help != "help" || got.DocumentationURL != "https://example.invalid/rule" {
		t.Fatalf("descriptor defaults missing: %+v", got)
	}
	if got := findings[1]; got.Path != "foreign.sh" || got.Rule != "explicit" || got.Explanation != "own" || got.Help != "own" || got.DocumentationURL != "https://example.invalid/own" {
		t.Fatalf("explicit diagnostic values overwritten: %+v", got)
	}
	if string(options) != `{"mode":"safe"}` || string(input.Options) != `{"unregistered":true}` || input.Available[0] != analyzer.Scripts || input.Scripts[0].Spans[0].End != 8 || input.Scripts[0].Comments[0].Text != "# original" || input.Scripts[0].Runtime.Shell != "bash" {
		t.Fatal("RunOne mutated parent input or registration")
	}
	if input.Scripts[0].Text != "echo ok" || input.Mappings[0].Scope != "original" || input.Mappings[0].Entries[0].Name != "original" || input.Dependencies[0].Uses != "original" || input.Files[0].Path != "/original" || input.Files[0].FS == nil || input.SourceRoots[0].Guest != "/original" {
		t.Fatal("RunOne shared mutable input collections")
	}
}

func TestRunInputValidationSurvivesNoRegistrations(t *testing.T) {
	input := analyzer.Input{Available: []analyzer.Fact{analyzer.Invocations}}
	if _, err := Run(t.Context(), input, nil); !errors.Is(err, analyzer.ErrInvalidCallGraph) {
		t.Fatalf("empty registration list skipped input validation: %v", err)
	}
	calls := 0
	registration := analyzer.Registration{Analyzer: testAnalyzer{descriptor: analyzer.Descriptor{Name: "never"}, run: func(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
		calls++
		return nil, nil
	}}}
	if _, err := RunOne(t.Context(), input, prepareOne(t, registration, input.Available)); !errors.Is(err, analyzer.ErrInvalidCallGraph) || calls != 0 {
		t.Fatalf("RunOne skipped input validation: calls=%d error=%v", calls, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := RunOne(ctx, analyzer.Input{}, prepareOne(t, registration, nil)); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("RunOne started canceled work: calls=%d error=%v", calls, err)
	}
}

type cancelingDescriptorAnalyzer struct {
	cancel context.CancelFunc
	calls  *int
}

func (a cancelingDescriptorAnalyzer) Descriptor() analyzer.Descriptor {
	a.cancel()
	return analyzer.Descriptor{Name: "canceled-before-analysis"}
}

func (a cancelingDescriptorAnalyzer) Analyze(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
	*a.calls++
	return nil, nil
}

func TestRunOneDoesNotStartAnalysisAfterPreparationCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	registration := analyzer.Registration{Analyzer: cancelingDescriptorAnalyzer{cancel: cancel, calls: &calls}}
	if _, err := RunOne(ctx, analyzer.Input{}, prepareOne(t, registration, nil)); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("canceled preparation started analyzer: calls=%d error=%v", calls, err)
	}
}

func prepareOne(t *testing.T, registration analyzer.Registration, available []analyzer.Fact) PreparedRegistration {
	t.Helper()
	prepared, err := Prepare(registration, available)
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func TestPrepareValidatesRegistration(t *testing.T) {
	for _, tc := range []struct {
		name         string
		registration analyzer.Registration
		available    []analyzer.Fact
		want         string
	}{
		{name: "nil analyzer", want: "nil analyzer registration"},
		{name: "typed nil analyzer", registration: analyzer.Registration{Analyzer: (*testAnalyzer)(nil)}, want: "nil analyzer registration"},
		{name: "empty name", registration: analyzer.Registration{Analyzer: testAnalyzer{}}, want: "name must not be empty"},
		{name: "unsupported requirement", registration: analyzer.Registration{Analyzer: testAnalyzer{descriptor: analyzer.Descriptor{Name: "future", Requires: []analyzer.Fact{"future"}}}}, want: `requires unsupported fact "future"`},
		{name: "unavailable requirement", registration: analyzer.Registration{Analyzer: testAnalyzer{descriptor: analyzer.Descriptor{Name: "source", Requires: []analyzer.Fact{analyzer.Source}}}}, want: `requires unavailable fact "source"`},
		{name: "invalid options", registration: analyzer.Registration{Analyzer: testAnalyzer{descriptor: analyzer.Descriptor{Name: "options"}}, Options: json.RawMessage("{")}, want: `analyzer "options" options:`},
		{name: "schema mismatch", registration: analyzer.Registration{Analyzer: testAnalyzer{descriptor: analyzer.Descriptor{Name: "options", OptionsSchema: json.RawMessage(`{"required":["mode"]}`)}}, Options: json.RawMessage(`{}`)}, want: `analyzer "options" options:`},
		{name: "invalid schema", registration: analyzer.Registration{Analyzer: testAnalyzer{descriptor: analyzer.Descriptor{Name: "options", OptionsSchema: json.RawMessage("{")}}}, want: `analyzer "options" options:`},
		{name: "unsupported available", registration: analyzer.Registration{Analyzer: testAnalyzer{descriptor: analyzer.Descriptor{Name: "facts"}}}, available: []analyzer.Fact{"future"}, want: "unsupported available fact"},
		{name: "duplicate available", registration: analyzer.Registration{Analyzer: testAnalyzer{descriptor: analyzer.Descriptor{Name: "facts"}}}, available: []analyzer.Fact{analyzer.Source, analyzer.Source}, want: "duplicate available fact"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Prepare(tc.registration, tc.available); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Prepare error=%v, want %q", err, tc.want)
			}
		})
	}
}

func TestRunRejectsUnsupportedAndDuplicateAvailableFactsPerRegistration(t *testing.T) {
	for _, tc := range []struct {
		name      string
		available []analyzer.Fact
		want      string
	}{
		{name: "unsupported", available: []analyzer.Fact{"future"}, want: `unsupported available fact "future"`},
		{name: "duplicate", available: []analyzer.Fact{analyzer.Source, analyzer.Source}, want: `duplicate available fact "source"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			registration := analyzer.Registration{Analyzer: testAnalyzer{descriptor: analyzer.Descriptor{Name: "never"}, run: func(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
				calls.Add(1)
				return nil, nil
			}}}
			findings, err := Run(t.Context(), analyzer.Input{Available: tc.available}, []analyzer.Registration{registration, registration})
			if len(findings) != 0 || calls.Load() != 0 || err == nil || strings.Count(err.Error(), tc.want) != 2 {
				t.Fatalf("invalid facts did not surface per registration: calls=%d findings=%v error=%v", calls.Load(), findings, err)
			}
		})
	}
}

type descriptorCountingAnalyzer struct {
	descriptor analyzer.Descriptor
	calls      int
	run        func(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error)
}

func (a *descriptorCountingAnalyzer) Descriptor() analyzer.Descriptor {
	a.calls++
	return a.descriptor
}

func (a *descriptorCountingAnalyzer) Analyze(ctx context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	return a.run(ctx, input)
}

func TestPreparedRegistrationOwnsMetadataAndRequiresExactFacts(t *testing.T) {
	options := json.RawMessage(`{"mode":"safe"}`)
	available := []analyzer.Fact{analyzer.Source, analyzer.Mappings}
	calls := 0
	a := &descriptorCountingAnalyzer{descriptor: analyzer.Descriptor{Name: "original", Requires: []analyzer.Fact{analyzer.Source}, OptionsSchema: json.RawMessage(`{"type":"object","required":["mode"]}`)}, run: func(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
		calls++
		if string(input.Options) != `{"mode":"safe"}` {
			t.Fatalf("prepared options changed: %s", input.Options)
		}
		input.Options[0] = '!'
		return []diagnostic.Diagnostic{{Message: "ok"}}, nil
	}}
	prepared := prepareOne(t, analyzer.Registration{Analyzer: a, Options: options}, available)
	options[0] = '!'
	available[0] = analyzer.Execution
	a.descriptor.Name = "changed"
	a.descriptor.Requires[0] = "future"
	a.descriptor.OptionsSchema[0] = '!'
	for range 2 {
		findings, err := RunOne(t.Context(), analyzer.Input{Available: []analyzer.Fact{analyzer.Mappings, analyzer.Source}}, prepared)
		if err != nil || len(findings) != 1 || findings[0].Rule != "original" {
			t.Fatalf("prepared metadata changed: %v %v", findings, err)
		}
	}
	if a.calls != 1 || calls != 2 {
		t.Fatalf("descriptor validated repeatedly: descriptor=%d analyze=%d", a.calls, calls)
	}
	for _, facts := range [][]analyzer.Fact{nil, {analyzer.Source}, {analyzer.Source, analyzer.Mappings, analyzer.Dependencies}, {analyzer.Source, analyzer.Source, analyzer.Mappings}} {
		if _, err := RunOne(t.Context(), analyzer.Input{Available: facts}, prepared); err == nil {
			t.Fatalf("prepared registration accepted mismatched facts: %v", facts)
		}
	}
	if _, err := RunOne(t.Context(), analyzer.Input{}, PreparedRegistration{}); err == nil || !strings.Contains(err.Error(), "unprepared") {
		t.Fatalf("zero prepared registration accepted: %v", err)
	}
	if calls != 2 {
		t.Fatalf("invalid prepared input executed: %d calls", calls)
	}
}

func TestRunOneClonesInvocationAndActionInputFacts(t *testing.T) {
	root, callID := analyzer.OccurrenceID(strings.Repeat("1", 64)), analyzer.OccurrenceID(strings.Repeat("2", 64))
	runID, inputID := analyzer.OccurrenceID(strings.Repeat("3", 64)), analyzer.OccurrenceID(strings.Repeat("4", 64))
	entry := source.Reference{Document: "workflow", Revision: "r", Node: "document"}
	target := source.Reference{Document: "action", Revision: "r", Node: "document"}
	outcome, err := analyzer.NewCallOutcome(analyzer.ExpandedCall{Target: target})
	if err != nil {
		t.Fatal(err)
	}
	occurrences := []analyzer.ScriptOccurrence{
		{ID: runID, Owner: callID, Ancestry: []analyzer.OccurrenceID{root, callID}, Depth: 1, Declaration: source.Reference{Document: "action", Revision: "r", Node: "run"}, Kind: analyzer.RunScript},
		{ID: inputID, Owner: callID, Ancestry: []analyzer.OccurrenceID{root, callID}, Depth: 1, Declaration: source.Reference{Document: "action", Revision: "r", Node: "input"}, Kind: analyzer.ActionInputScript, Input: "script"},
	}
	graph, err := analyzer.NewCallGraph(analyzer.CallGraphData{
		Entry:  analyzer.InvocationEntry{ID: root, Kind: analyzer.WorkflowEntry, Reference: entry},
		Budget: analyzer.InvocationBudget{MaxDepth: 1, MaxCalls: 1, MaxScripts: 2}, Complete: true,
		Calls: []analyzer.Call{{ID: callID, Parent: root, Ancestry: []analyzer.OccurrenceID{root}, Depth: 1, Site: source.Reference{Document: "workflow", Revision: "r", Node: "call"}, Condition: analyzer.CallEnabled, Outcome: outcome}}, Scripts: occurrences,
	})
	if err != nil {
		t.Fatal(err)
	}
	input := analyzer.Input{Reference: entry, CallGraph: graph, Available: []analyzer.Fact{analyzer.Scripts, analyzer.Invocations}, SelectedScripts: []analyzer.OccurrenceID{runID, inputID},
		Invoked:      []analyzer.Script{{Reference: occurrences[0].Declaration, Occurrence: occurrences[0], Text: "original", Spans: []analyzer.Span{{End: 8}}, Comments: []analyzer.SourceComment{{Text: "original"}}, Runtime: &analyzer.Runtime{Shell: "bash"}}},
		ActionInputs: []analyzer.ActionInput{{Reference: occurrences[1].Declaration, Occurrence: occurrences[1], Text: "original", Spans: []analyzer.Span{{End: 8}}, Comments: []analyzer.SourceComment{{Text: "original"}}, Site: analyzer.ScriptInput{Action: "owner/action", Input: "script", Language: "javascript"}}},
	}
	want := graph.Snapshot()
	prepared := prepareOne(t, analyzer.Registration{Analyzer: testAnalyzer{descriptor: analyzer.Descriptor{Name: "mutator"}, run: func(_ context.Context, supplied analyzer.Input) ([]diagnostic.Diagnostic, error) {
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
	}}}, input.Available)
	if _, err := RunOne(t.Context(), input, prepared); err != nil {
		t.Fatal(err)
	}
	if input.SelectedScripts[0] != runID || input.Invoked[0].Text != "original" || input.Invoked[0].Occurrence.Ancestry[0] != root || input.Invoked[0].Spans[0].End != 8 || input.Invoked[0].Comments[0].Text != "original" || input.Invoked[0].Runtime.Shell != "bash" {
		t.Fatal("RunOne mutated invoked script or selection")
	}
	if input.ActionInputs[0].Text != "original" || input.ActionInputs[0].Occurrence.Ancestry[0] != root || input.ActionInputs[0].Spans[0].End != 8 || input.ActionInputs[0].Comments[0].Text != "original" || input.ActionInputs[0].Site.Language != "javascript" || !reflect.DeepEqual(graph.Snapshot(), want) {
		t.Fatal("RunOne mutated action input or graph")
	}
}

func TestRunOneClonesSharedReturnedFindings(t *testing.T) {
	shared := []diagnostic.Diagnostic{{Message: "cached"}}
	prepared := prepareOne(t, analyzer.Registration{Analyzer: testAnalyzer{descriptor: analyzer.Descriptor{Name: "shared", Explanation: "why", Help: "help", DocumentationURL: "https://example.invalid"}, run: func(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) { return shared, nil }}}, nil)
	var group sync.WaitGroup
	start := make(chan struct{})
	for i := range 16 {
		group.Go(func() {
			<-start
			path := fmt.Sprintf("%d.yml", i)
			findings, err := RunOne(t.Context(), analyzer.Input{Path: path}, prepared)
			if err != nil || len(findings) != 1 || findings[0].Path != path || findings[0].Rule != "shared" {
				t.Errorf("shared output corrupted: %v %v", findings, err)
			}
		})
	}
	close(start)
	group.Wait()
	if !reflect.DeepEqual(shared, []diagnostic.Diagnostic{{Message: "cached"}}) {
		t.Fatalf("RunOne mutated analyzer-owned findings: %+v", shared)
	}
}

func TestRunOneRetainsCancellationAndPartialFindings(t *testing.T) {
	for _, analyzerErr := range []error{nil, errors.New("partial failure"), fmt.Errorf("wrapped: %w", context.Canceled)} {
		t.Run(fmt.Sprint(analyzerErr), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			prepared := prepareOne(t, analyzer.Registration{Analyzer: testAnalyzer{descriptor: analyzer.Descriptor{Name: "canceling"}, run: func(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
				cancel()
				return []diagnostic.Diagnostic{{Message: "partial"}}, analyzerErr
			}}}, nil)
			findings, err := RunOne(ctx, analyzer.Input{}, prepared)
			if len(findings) != 1 || !errors.Is(err, context.Canceled) || analyzerErr != nil && !errors.Is(err, analyzerErr) || strings.Count(err.Error(), context.Canceled.Error()) != 1 {
				t.Fatalf("cancellation lost or duplicated: %v %v", findings, err)
			}
		})
	}
}

func TestRunOneLeavesAnalyzerPanicsToCaller(t *testing.T) {
	prepared := prepareOne(t, analyzer.Registration{Analyzer: testAnalyzer{descriptor: analyzer.Descriptor{Name: "panicking"}, run: func(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
		panic("analyzer bug")
	}}}, nil)
	defer func() {
		if got := recover(); got != "analyzer bug" {
			t.Errorf("analyzer panic changed: %v", got)
		}
	}()
	_, _ = RunOne(t.Context(), analyzer.Input{}, prepared)
	t.Fatal("analyzer panic was swallowed")
}
