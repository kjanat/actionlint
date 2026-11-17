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

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/invocationfixture"
	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/commands"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
	"actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/model/workflows/mappings"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestRunOneOwnsIsolationDefaultsAndPartialError(t *testing.T) {
	t.Parallel()
	failure := errors.New("incomplete")
	options := json.RawMessage(`{"mode":"safe"}`)
	input := checkinputs.AnalyzerInput{
		Path: "unit.yml", Options: json.RawMessage(`{"unregistered":true}`), AvailableInputs: []rule.InputKind{rule.Scripts},
		Mappings:     []facts.Mapping{{Scope: "original", Entries: []mappings.Declaration{{Name: "original"}}}},
		Dependencies: []facts.Dependency{{Uses: "original"}}, Files: []checkinputs.FileSystem{{Path: "/original", FS: fstest.MapFS{}}},
		SourceRoots: []checkinputs.SourceRoot{{Host: "/host", Guest: "/original"}},
		Scripts:     []checkinputs.Script{{Text: "echo ok", Spans: []sourcemap.Span{{End: 8}}, Comments: []facts.SourceComment{{Text: "# original"}}, Runtime: &scripts.Execution{Shell: scripts.Shell{Name: "bash"}}}},
	}
	registration := checkinputs.Registration{Options: options, Checker: testAnalyzer{
		descriptor: checkinputs.Descriptor{Descriptor: rule.Descriptor{Name: "single", Explanation: "explain", Help: "help", DocumentationURL: "https://example.invalid/rule"}},
		run: func(_ context.Context, supplied checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
			if string(supplied.Options) != string(options) {
				t.Fatalf("input options replaced registered options: %s", supplied.Options)
			}
			supplied.Options[0] = '!'
			supplied.AvailableInputs[0] = rule.Source
			supplied.Scripts[0].Spans[0].End = 99
			supplied.Scripts[0].Comments[0].Text = "changed"
			supplied.Scripts[0].Runtime.Shell.Name = "changed"
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
	if string(options) != `{"mode":"safe"}` || string(input.Options) != `{"unregistered":true}` || input.AvailableInputs[0] != rule.Scripts || input.Scripts[0].Spans[0].End != 8 || input.Scripts[0].Comments[0].Text != "# original" || input.Scripts[0].Runtime.Shell.Name != "bash" {
		t.Fatal("RunOne mutated parent input or registration")
	}
	if input.Scripts[0].Text != "echo ok" || input.Mappings[0].Scope != "original" || input.Mappings[0].Entries[0].Name != "original" || input.Dependencies[0].Uses != "original" || input.Files[0].Path != "/original" || input.Files[0].FS == nil || input.SourceRoots[0].Guest != "/original" {
		t.Fatal("RunOne shared mutable input collections")
	}
}

func TestRunOneDetachesAnalyzerDiagnostics(t *testing.T) {
	t.Parallel()
	retained := []results.Diagnostic{{
		Message: "original", Reference: &sourcetext.Reference{Document: "original"},
		Lifecycle: &results.LifecycleReport{Policies: []results.LifecyclePolicyReport{{ID: "original"}}},
		Fixes:     []results.Fix{{Revisions: map[string]string{"file": "original"}, Edits: []results.Edit{{Replacement: "original"}}}},
	}}
	registration := checkinputs.Registration{Checker: testAnalyzer{
		descriptor: checkinputs.Descriptor{Descriptor: rule.Descriptor{Name: "example"}},
		run:        func(context.Context, checkinputs.AnalyzerInput) ([]results.Diagnostic, error) { return retained, nil },
	}}
	got, err := RunOne(t.Context(), checkinputs.AnalyzerInput{}, prepareOne(t, registration, nil))
	if err != nil || len(got) != 1 {
		t.Fatalf("findings=%+v error=%v", got, err)
	}
	got[0].Reference.Document = "changed"
	got[0].Lifecycle.Policies[0].ID = "changed"
	got[0].Fixes[0].Revisions["file"] = "changed"
	got[0].Fixes[0].Edits[0].Replacement = "changed"
	if retained[0].Reference.Document != "original" || retained[0].Lifecycle.Policies[0].ID != "original" || retained[0].Fixes[0].Revisions["file"] != "original" || retained[0].Fixes[0].Edits[0].Replacement != "original" {
		t.Fatalf("published diagnostic mutated analyzer-owned data: %+v", retained[0])
	}
}

func TestForwardedAnalyzerUsesSharedAdmission(t *testing.T) {
	t.Parallel()
	failure := errors.New("runtime failure")
	input := checkinputs.AnalyzerInput{Path: "workflow.yml", AvailableInputs: []rule.InputKind{rule.Scripts}, Scripts: []checkinputs.Script{{Text: "original"}}}
	calls := 0
	prepared, err := PrepareForwarding(checkinputs.Registration{Checker: testAnalyzer{
		descriptor: checkinputs.Descriptor{Descriptor: rule.Descriptor{Name: "forwarded", RequiredInputs: []rule.InputKind{rule.Scripts}}},
		run: func(_ context.Context, supplied checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
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
	input.AvailableInputs = append(input.AvailableInputs, rule.Invocations)
	if _, err := RunOne(t.Context(), input, prepared); !errors.Is(err, checkinputs.ErrInvalidCallGraph) || calls != 1 {
		t.Fatalf("forwarding bypassed graph admission: calls=%d error=%v", calls, err)
	}
}

func TestRunInputValidationSurvivesNoRegistrations(t *testing.T) {
	t.Parallel()
	input := checkinputs.AnalyzerInput{AvailableInputs: []rule.InputKind{rule.Invocations}}
	if _, err := Run(t.Context(), input, nil); !errors.Is(err, checkinputs.ErrInvalidCallGraph) {
		t.Fatalf("empty registration list skipped input validation: %v", err)
	}
	calls := 0
	registration := checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rule.Descriptor{Name: "never"}}, run: func(context.Context, checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
		calls++
		return nil, nil
	}}}
	if _, err := RunOne(t.Context(), input, prepareOne(t, registration, input.AvailableInputs)); !errors.Is(err, checkinputs.ErrInvalidCallGraph) || calls != 0 {
		t.Fatalf("RunOne skipped input validation: calls=%d error=%v", calls, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := RunOne(ctx, checkinputs.AnalyzerInput{}, prepareOne(t, registration, nil)); !errors.Is(err, context.Canceled) || calls != 0 {
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

func (a cancelingDescriptorAnalyzer) CheckDocument(context.Context, checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
	*a.calls++
	return nil, nil
}

func TestRunOneDoesNotStartAnalysisAfterPreparationCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	registration := checkinputs.Registration{Checker: cancelingDescriptorAnalyzer{cancel: cancel, calls: &calls}}
	if _, err := RunOne(ctx, checkinputs.AnalyzerInput{}, prepareOne(t, registration, nil)); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("canceled preparation started analyzer: calls=%d error=%v", calls, err)
	}
}

func prepareOne(t *testing.T, registration checkinputs.Registration, available []rule.InputKind) PreparedRegistration {
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
		available    []rule.InputKind
		want         string
	}{
		{name: "nil analyzer", want: "nil analyzer registration"},
		{name: "typed nil analyzer", registration: checkinputs.Registration{Checker: (*testAnalyzer)(nil)}, want: "nil analyzer registration"},
		{name: "empty name", registration: checkinputs.Registration{Checker: testAnalyzer{}}, want: "name must not be empty"},
		{name: "unsupported requirement", registration: checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rule.Descriptor{Name: "future", RequiredInputs: []rule.InputKind{"future"}}}}}, want: `requires unsupported input kind "future"`},
		{name: "unavailable requirement", registration: checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rule.Descriptor{Name: "source", RequiredInputs: []rule.InputKind{rule.Source}}}}}, want: `requires unavailable input kind "source"`},
		{name: "invalid options", registration: checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rule.Descriptor{Name: "options"}}}, Options: json.RawMessage("{")}, want: `analyzer "options" options:`},
		{name: "schema mismatch", registration: checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rule.Descriptor{Name: "options", OptionsSchema: json.RawMessage(`{"required":["mode"]}`)}}}, Options: json.RawMessage(`{}`)}, want: `analyzer "options" options:`},
		{name: "invalid schema", registration: checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rule.Descriptor{Name: "options", OptionsSchema: json.RawMessage("{")}}}}, want: `analyzer "options" options:`},
		{name: "unsupported available", registration: checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rule.Descriptor{Name: "facts"}}}}, available: []rule.InputKind{"future"}, want: "unsupported available input kind"},
		{name: "duplicate available", registration: checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rule.Descriptor{Name: "facts"}}}}, available: []rule.InputKind{rule.Source, rule.Source}, want: "duplicate available input kind"},
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
		available []rule.InputKind
		want      string
	}{
		{name: "unsupported", available: []rule.InputKind{"future"}, want: `unsupported available input kind "future"`},
		{name: "duplicate", available: []rule.InputKind{rule.Source, rule.Source}, want: `duplicate available input kind "source"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			registration := checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rule.Descriptor{Name: "never"}}, run: func(context.Context, checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
				calls.Add(1)
				return nil, nil
			}}}
			findings, err := Run(t.Context(), checkinputs.AnalyzerInput{AvailableInputs: tc.available}, []checkinputs.Registration{registration, registration})
			if len(findings) != 0 || calls.Load() != 0 || err == nil || strings.Count(err.Error(), tc.want) != 2 {
				t.Fatalf("invalid input kinds did not surface per registration: calls=%d findings=%v error=%v", calls.Load(), findings, err)
			}
		})
	}
}

type descriptorCountingAnalyzer struct {
	descriptor checkinputs.Descriptor
	calls      int
	run        func(context.Context, checkinputs.AnalyzerInput) ([]results.Diagnostic, error)
}

func (a *descriptorCountingAnalyzer) Descriptor() checkinputs.Descriptor {
	a.calls++
	return a.descriptor
}

func (a *descriptorCountingAnalyzer) CheckDocument(ctx context.Context, input checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
	return a.run(ctx, input)
}

func TestPreparedRegistrationOwnsMetadataAndRequiresExactFacts(t *testing.T) {
	t.Parallel()
	options := json.RawMessage(`{"mode":"safe"}`)
	available := []rule.InputKind{rule.Source, rule.Mappings}
	calls := 0
	a := &descriptorCountingAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rule.Descriptor{Name: "original", OptionsSchema: json.RawMessage(`{"type":"object","required":["mode"]}`), RequiredInputs: []rule.InputKind{rule.Source}}, InvocationDefinitions: []commands.Definition{{ID: "shell", Options: []commands.Option{{Long: "mode", Choices: []string{"original"}}}}}}, run: func(_ context.Context, input checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
		calls++
		if string(input.Options) != `{"mode":"safe"}` {
			t.Fatalf("prepared options changed: %s", input.Options)
		}
		input.Options[0] = '!'
		return []results.Diagnostic{{Message: "ok"}}, nil
	}}
	prepared := prepareOne(t, checkinputs.Registration{Checker: a, Options: options}, available)
	options[0] = '!'
	available[0] = rule.Execution
	a.descriptor.Name = "changed"
	a.descriptor.RequiredInputs[0] = "future"
	a.descriptor.OptionsSchema[0] = '!'
	a.descriptor.InvocationDefinitions[0].Options[0].Choices[0] = "changed"
	if got := prepared.data.descriptor.InvocationDefinitions[0].Options[0].Choices[0]; got != "original" {
		t.Fatalf("prepared invocation definition changed: %q", got)
	}
	for range 2 {
		findings, err := RunOne(t.Context(), checkinputs.AnalyzerInput{AvailableInputs: []rule.InputKind{rule.Mappings, rule.Source}}, prepared)
		if err != nil || len(findings) != 1 || findings[0].Rule != "original" {
			t.Fatalf("prepared metadata changed: %v %v", findings, err)
		}
	}
	if a.calls != 1 || calls != 2 {
		t.Fatalf("descriptor validated repeatedly: descriptor=%d analyze=%d", a.calls, calls)
	}
	for _, facts := range [][]rule.InputKind{nil, {rule.Source}, {rule.Source, rule.Mappings, rule.Dependencies}, {rule.Source, rule.Source, rule.Mappings}} {
		if _, err := RunOne(t.Context(), checkinputs.AnalyzerInput{AvailableInputs: facts}, prepared); err == nil {
			t.Fatalf("prepared registration accepted mismatched input kinds: %v", facts)
		}
	}
	if _, err := RunOne(t.Context(), checkinputs.AnalyzerInput{}, PreparedRegistration{}); err == nil || !strings.Contains(err.Error(), "unprepared") {
		t.Fatalf("zero prepared registration accepted: %v", err)
	}
	if calls != 2 {
		t.Fatalf("invalid prepared input executed: %d calls", calls)
	}
}

func TestRunOneClonesInvocationAndActionInputFacts(t *testing.T) {
	t.Parallel()
	graph := invocationfixture.Graph(t, invocationfixture.Script{Node: "run"}, invocationfixture.Script{Node: "input", Input: "script"})
	occurrences := graph.Scripts()
	runID, inputID, entry := occurrences[0].ID, occurrences[1].ID, graph.Entry().Reference
	input := checkinputs.AnalyzerInput{
		Reference: entry, CallGraph: graph, AvailableInputs: []rule.InputKind{rule.Scripts, rule.Invocations}, SelectedScripts: []invocation.ID{runID, inputID},
		Invoked:      []checkinputs.Script{{Reference: occurrences[0].Declaration, Text: "original", Spans: []sourcemap.Span{{End: 8}}, Comments: []facts.SourceComment{{Text: "original"}}, Occurrence: occurrences[0], Runtime: &scripts.Execution{Shell: scripts.Shell{Name: "bash"}}}},
		ActionInputs: []checkinputs.ActionInput{{Reference: occurrences[1].Declaration, Text: "original", Spans: []sourcemap.Span{{End: 8}}, Comments: []facts.SourceComment{{Text: "original"}}, Occurrence: occurrences[1], Site: checkinputs.ScriptInput{Action: "owner/action", Input: "script", Language: "javascript"}}},
	}
	prepared := prepareOne(t, checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rule.Descriptor{Name: "mutator"}}, run: func(_ context.Context, supplied checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
		supplied.SelectedScripts[0] = "changed"
		supplied.Invoked[0].Text = "changed"
		supplied.Invoked[0].Occurrence.Owner = "changed"
		supplied.Invoked[0].Spans[0].End = 99
		supplied.Invoked[0].Comments[0].Text = "changed"
		supplied.Invoked[0].Runtime.Shell.Name = "changed"
		supplied.ActionInputs[0].Text = "changed"
		supplied.ActionInputs[0].Occurrence.Owner = "changed"
		supplied.ActionInputs[0].Spans[0].End = 99
		supplied.ActionInputs[0].Comments[0].Text = "changed"
		supplied.ActionInputs[0].Site.Language = "changed"
		return nil, nil
	}}}, input.AvailableInputs)
	if _, err := RunOne(t.Context(), input, prepared); err != nil {
		t.Fatal(err)
	}
	if input.SelectedScripts[0] != runID || input.Invoked[0].Text != "original" || input.Invoked[0].Occurrence != occurrences[0] || input.Invoked[0].Spans[0].End != 8 || input.Invoked[0].Comments[0].Text != "original" || input.Invoked[0].Runtime.Shell.Name != "bash" {
		t.Fatal("RunOne mutated invoked script or selection")
	}
	if input.ActionInputs[0].Text != "original" || input.ActionInputs[0].Occurrence != occurrences[1] || input.ActionInputs[0].Spans[0].End != 8 || input.ActionInputs[0].Comments[0].Text != "original" || input.ActionInputs[0].Site.Language != "javascript" {
		t.Fatal("RunOne mutated action input")
	}
}

func TestRunOneClonesSharedReturnedFindings(t *testing.T) {
	t.Parallel()
	shared := []results.Diagnostic{{Message: "cached"}}
	prepared := prepareOne(t, checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rule.Descriptor{Name: "shared", Explanation: "why", Help: "help", DocumentationURL: "https://example.invalid"}}, run: func(context.Context, checkinputs.AnalyzerInput) ([]results.Diagnostic, error) { return shared, nil }}}, nil)
	var group sync.WaitGroup
	start := make(chan struct{})
	for i := range 16 {
		group.Go(func() {
			<-start
			path := fmt.Sprintf("%d.yml", i)
			findings, err := RunOne(t.Context(), checkinputs.AnalyzerInput{Path: path}, prepared)
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
			prepared := prepareOne(t, checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rule.Descriptor{Name: "canceling"}}, run: func(context.Context, checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
				cancel()
				return []results.Diagnostic{{Message: "partial"}}, analyzerErr
			}}}, nil)
			findings, err := RunOne(ctx, checkinputs.AnalyzerInput{}, prepared)
			if len(findings) != 1 || !errors.Is(err, context.Canceled) || analyzerErr != nil && !errors.Is(err, analyzerErr) || strings.Count(err.Error(), context.Canceled.Error()) != 1 {
				t.Fatalf("cancellation lost or duplicated: %v %v", findings, err)
			}
		})
	}
}

func TestRunOneLeavesAnalyzerPanicsToCaller(t *testing.T) {
	t.Parallel()
	prepared := prepareOne(t, checkinputs.Registration{Checker: testAnalyzer{descriptor: checkinputs.Descriptor{Descriptor: rule.Descriptor{Name: "panicking"}}, run: func(context.Context, checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
		panic("analyzer bug")
	}}}, nil)
	defer func() {
		if got := recover(); got != "analyzer bug" {
			t.Errorf("analyzer panic changed: %v", got)
		}
	}()
	_, _ = RunOne(t.Context(), checkinputs.AnalyzerInput{}, prepared)
	t.Fatal("analyzer panic was swallowed")
}
