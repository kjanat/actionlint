package execution

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
)

type factAdapterAnalyzer struct {
	descriptor analyzer.Descriptor
	run        func(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error)
}

func (a factAdapterAnalyzer) Descriptor() analyzer.Descriptor { return a.descriptor }
func (a factAdapterAnalyzer) Analyze(ctx context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	return a.run(ctx, input)
}

func twoRunFactBatch(t *testing.T) Batch {
	t.Helper()
	graph, collection := batchSourceFixture(t, []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo first\n      - run: echo second\n"))
	batch, err := NewBatch(graph, collection)
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

func TestFactAnalyzerJoinsUnitErrorsAndKeepsPartialFindings(t *testing.T) {
	batch := twoRunFactBatch(t)
	failures := []error{errors.New("first failed"), errors.New("second failed")}
	calls := 0
	adapter := FactAnalyzer{Analyzer: factAdapterAnalyzer{
		descriptor: analyzer.Descriptor{Name: "partial", Explanation: "why", Help: "help", DocumentationURL: "https://example.invalid/rule"},
		run: func(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
			index := calls
			calls++
			return []diagnostic.Diagnostic{{Message: input.Scripts[0].Text}}, failures[index]
		},
	}}
	findings, err := adapter.AnalyzeScripts(t.Context(), batch)
	if calls != 2 || len(findings) != 2 || !errors.Is(err, failures[0]) || !errors.Is(err, failures[1]) {
		t.Fatalf("partial unit results lost: calls=%d findings=%v error=%v", calls, findings, err)
	}
	if findings[0].Message != "echo first" || findings[1].Message != "echo second" || strings.Index(err.Error(), "first failed") > strings.Index(err.Error(), "second failed") {
		t.Fatalf("unit results lost execution order: %v %v", findings, err)
	}
	for index, unit := range batch.Scripts() {
		fact, exists := batch.Fact(index, false)
		if !exists || !strings.Contains(err.Error(), unit.Path) || !strings.Contains(err.Error(), string(fact.Occurrence.ID)) {
			t.Fatalf("unit error lost path or occurrence: %v", err)
		}
	}
	for _, finding := range findings {
		if finding.Rule != "partial" || finding.Explanation != "why" || finding.Help != "help" || finding.DocumentationURL != "https://example.invalid/rule" {
			t.Fatalf("fact adapter bypassed descriptor defaults: %+v", finding)
		}
	}
}

func TestFactAnalyzerCancellationKeepsPriorErrorAndFinding(t *testing.T) {
	batch := twoRunFactBatch(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	failure := errors.New("first failed")
	calls := 0
	adapter := FactAnalyzer{Analyzer: factAdapterAnalyzer{
		descriptor: analyzer.Descriptor{Name: "partial"},
		run: func(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
			calls++
			cancel()
			return []diagnostic.Diagnostic{{Message: "partial"}}, failure
		},
	}}
	findings, err := adapter.AnalyzeScripts(ctx, batch)
	if calls != 1 || len(findings) != 1 || !errors.Is(err, failure) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel lost partial results or ran later unit: calls=%d findings=%v error=%v", calls, findings, err)
	}
}

func TestFactAnalyzerEnforcesRegistrationRequirements(t *testing.T) {
	batch := twoRunFactBatch(t)
	for _, tc := range []struct {
		name       string
		descriptor analyzer.Descriptor
		want       string
	}{
		{"missing source", analyzer.Descriptor{Name: "source", Requires: []analyzer.Fact{analyzer.Source}}, `requires unavailable fact "source"`},
		{"unsupported fact", analyzer.Descriptor{Name: "future", Requires: []analyzer.Fact{"future"}}, `requires unsupported fact "future"`},
		{"missing name", analyzer.Descriptor{}, "analyzer name must not be empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			adapter := FactAnalyzer{Analyzer: factAdapterAnalyzer{descriptor: tc.descriptor, run: func(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
				calls++
				return nil, nil
			}}}
			findings, err := adapter.AnalyzeScripts(t.Context(), batch)
			if calls != 0 || len(findings) != 0 || err == nil || strings.Count(err.Error(), tc.want) != 1 {
				t.Fatalf("invalid registration executed: calls=%d findings=%v error=%v", calls, findings, err)
			}
		})
	}
	t.Run("nil analyzer", func(t *testing.T) {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("nil analyzer panicked instead of returning validation error: %v", recovered)
			}
		}()
		if _, err := (FactAnalyzer{}).AnalyzeScripts(t.Context(), batch); err == nil || !strings.Contains(err.Error(), "nil analyzer registration") {
			t.Fatalf("nil analyzer was accepted: %v", err)
		}
	})
}

type factDescriptorCounter struct {
	descriptor      analyzer.Descriptor
	descriptorCalls int
	analyzeCalls    int
}

func (a *factDescriptorCounter) Descriptor() analyzer.Descriptor {
	a.descriptorCalls++
	return a.descriptor
}

func (a *factDescriptorCounter) Analyze(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
	a.analyzeCalls++
	return nil, nil
}

func TestFactAnalyzerPreparesRegistrationOnceIncludingNoEligibleUnits(t *testing.T) {
	for _, tc := range []struct {
		name, steps string
		wantCalls   int
	}{
		{name: "two scripts", steps: "      - run: echo first\n      - run: echo second\n", wantCalls: 2},
		{name: "no scripts"},
		{name: "only action input", steps: "      - uses: actions/github-script@v7\n        with: {script: console.log(1)}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n" + tc.steps
			if tc.name == "no scripts" {
				content = "on: push\njobs:\n  test:\n    uses: owner/repository/.github/workflows/test.yml@main\n"
			}
			graph, collection := batchSourceFixture(t, []byte(content))
			batch, err := NewBatch(graph, collection)
			if err != nil {
				t.Fatal(err)
			}
			for _, valid := range []bool{false, true} {
				a := &factDescriptorCounter{descriptor: analyzer.Descriptor{Name: "prepared", Requires: []analyzer.Fact{analyzer.Scripts, analyzer.Invocations}, OptionsSchema: json.RawMessage(`{"required":["mode"]}`)}}
				options := json.RawMessage(`{}`)
				if valid {
					options = json.RawMessage(`{"mode":"safe"}`)
				}
				findings, err := (FactAnalyzer{Analyzer: a, Options: options}).AnalyzeScripts(t.Context(), batch)
				if a.descriptorCalls != 1 || len(findings) != 0 {
					t.Fatalf("registration prepared repeatedly: descriptor=%d findings=%v", a.descriptorCalls, findings)
				}
				if valid && (err != nil || a.analyzeCalls != tc.wantCalls) || !valid && (err == nil || a.analyzeCalls != 0 || strings.Count(err.Error(), `analyzer "prepared" options:`) != 1) {
					t.Fatalf("valid=%v calls=%d error=%v", valid, a.analyzeCalls, err)
				}
			}
			if _, err := (FactAnalyzer{}).AnalyzeScripts(t.Context(), batch); err == nil || strings.Count(err.Error(), "nil analyzer registration") != 1 {
				t.Fatalf("batch accepted nil registration: %v", err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			a := &factDescriptorCounter{descriptor: analyzer.Descriptor{Name: "canceled"}}
			if _, err := (FactAnalyzer{Analyzer: a}).AnalyzeScripts(ctx, batch); !errors.Is(err, context.Canceled) || a.analyzeCalls != 0 {
				t.Fatalf("canceled batch executed or lost cancellation: calls=%d error=%v", a.analyzeCalls, err)
			}
		})
	}
}

func TestFactAnalyzerCancellationBoundaryAndDeduplication(t *testing.T) {
	for _, tc := range []struct {
		name                string
		cancelBefore        bool
		cancelAt, wantCalls int
		ownError            bool
	}{
		{name: "before first", cancelBefore: true},
		{name: "during last successful", cancelAt: 2, wantCalls: 2},
		{name: "during first successful", cancelAt: 1, wantCalls: 1},
		{name: "during first with cancellation error", cancelAt: 1, wantCalls: 1, ownError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancelBefore {
				cancel()
			}
			calls := 0
			adapter := FactAnalyzer{Analyzer: factAdapterAnalyzer{descriptor: analyzer.Descriptor{Name: "canceling"}, run: func(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
				calls++
				if calls == tc.cancelAt {
					cancel()
				}
				if tc.ownError {
					return []diagnostic.Diagnostic{{Message: "partial"}}, context.Canceled
				}
				return []diagnostic.Diagnostic{{Message: "partial"}}, nil
			}}}
			findings, err := adapter.AnalyzeScripts(ctx, twoRunFactBatch(t))
			if calls != tc.wantCalls || len(findings) != tc.wantCalls || !errors.Is(err, context.Canceled) || strings.Count(err.Error(), context.Canceled.Error()) != 1 {
				t.Fatalf("cancellation lost or duplicated: calls=%d findings=%v error=%v", calls, findings, err)
			}
		})
	}
}

func TestFactAnalyzerValidatesRegisteredOptions(t *testing.T) {
	batch := twoRunFactBatch(t)
	for _, tc := range []struct{ name, schema, options string }{
		{"invalid JSON", "", "{"},
		{"schema mismatch", `{"type":"object","required":["mode"]}`, `{}`},
		{"missing required options", `{"type":"object","required":["mode"]}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			adapter := FactAnalyzer{Options: json.RawMessage(tc.options), Analyzer: factAdapterAnalyzer{
				descriptor: analyzer.Descriptor{Name: "options", OptionsSchema: json.RawMessage(tc.schema)},
				run: func(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
					calls++
					return nil, nil
				},
			}}
			if _, err := adapter.AnalyzeScripts(t.Context(), batch); calls != 0 || err == nil || !strings.Contains(err.Error(), `analyzer "options" options:`) {
				t.Fatalf("invalid options executed: calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestFactAnalyzerProjectsOnlySelectedScriptFacts(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(map[bool]string{false: "entry", true: "invoked"}[nested], func(t *testing.T) {
			graph, collection, snapshot, _ := actionBatchFixture(t, false)
			if nested {
				graph, collection = nestedActionBatch(t, collection, snapshot)
			}
			batch, err := NewBatch(graph, collection)
			if err != nil {
				t.Fatal(err)
			}
			units := batch.Scripts()
			calls := 0
			adapter := FactAnalyzer{Analyzer: factAdapterAnalyzer{
				descriptor: analyzer.Descriptor{Name: "selected", Requires: []analyzer.Fact{analyzer.Scripts, analyzer.Invocations}},
				run: func(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
					unit := units[calls]
					calls++
					if err := input.ValidateInvocations(); err != nil {
						t.Fatal(err)
					}
					if input.Reference != batch.CallGraph().Entry().Reference || input.Path != unit.Path || input.Content != "" || input.Kind != "" || input.ParseStatus != "" || !slices.Equal(input.Available, []analyzer.Fact{analyzer.Scripts, analyzer.Invocations}) {
						t.Fatalf("script call invented a source/header or lost unit path: %+v", input)
					}
					scripts := slices.Concat(input.Scripts, input.Invoked)
					if len(scripts) != 1 || len(input.SelectedScripts) != 1 || input.SelectedScripts[0] != scripts[0].Occurrence.ID || scripts[0].Reference != unit.Reference || scripts[0].Runtime != nil || len(input.ActionInputs) != 0 {
						t.Fatalf("selected script call lost occurrence facts: %+v", input)
					}
					if nested && len(input.Invoked) != 1 || !nested && len(input.Scripts) != 1 {
						t.Fatalf("script is in wrong input collection: %+v", input)
					}
					return []diagnostic.Diagnostic{{Message: "selected"}}, nil
				},
			}}
			findings, err := adapter.AnalyzeScripts(t.Context(), batch)
			if err != nil || calls != len(units) || len(findings) != len(units) {
				t.Fatalf("unit selection changed: calls=%d findings=%v error=%v", calls, findings, err)
			}
			for i, finding := range findings {
				if finding.Path != units[i].Path || !reflect.DeepEqual(finding.Owner, units[i].Owner) || !reflect.DeepEqual(finding.IgnoreOrigin, units[i].Origin) {
					t.Fatalf("shared defaults lost display/provenance: %+v", finding)
				}
			}
		})
	}
}

func TestFactAnalyzerInputMutationDoesNotReachLaterUnitsOrBatch(t *testing.T) {
	batch := twoRunFactBatch(t)
	want := batch.CallGraph().Snapshot()
	options := json.RawMessage(`{"mode":"safe"}`)
	calls := 0
	adapter := FactAnalyzer{Options: options, Analyzer: factAdapterAnalyzer{
		descriptor: analyzer.Descriptor{Name: "mutating", OptionsSchema: json.RawMessage(`{"type":"object","required":["mode"]}`)},
		run: func(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
			if string(input.Options) != `{"mode":"safe"}` || input.Available[0] != analyzer.Scripts || !reflect.DeepEqual(input.CallGraph.Snapshot(), want) {
				t.Fatalf("earlier analyzer call corrupted current input: %+v", input)
			}
			calls++
			input.Options[0] = '!'
			input.Available[0] = analyzer.Source
			input.SelectedScripts[0] = "forged"
			input.Scripts[0].Occurrence.Ancestry[0] = "forged"
			input.Scripts[0].Spans[0].End = 999
			snapshot := input.CallGraph.Snapshot()
			snapshot.Scripts[0].Ancestry[0] = "forged"
			return nil, nil
		},
	}}
	if _, err := adapter.AnalyzeScripts(t.Context(), batch); err != nil || calls != 2 {
		t.Fatalf("mutation interrupted later unit: calls=%d error=%v", calls, err)
	}
	if string(options) != `{"mode":"safe"}` || !reflect.DeepEqual(batch.CallGraph().Snapshot(), want) {
		t.Fatal("analyzer mutated parent registration or batch graph")
	}
	for index := range batch.Scripts() {
		fact, exists := batch.Fact(index, false)
		if !exists || fact.Occurrence.Ancestry[0] != want.Entry.ID || fact.Spans[0].End == 999 {
			t.Fatal("analyzer mutated parent script facts")
		}
	}
}

func TestFactAnalyzerInvokedInputMutationDoesNotReachBatch(t *testing.T) {
	graph, collection, snapshot, _ := actionBatchFixture(t, false)
	graph, collection = nestedActionBatch(t, collection, snapshot)
	batch, err := NewBatch(graph, collection)
	if err != nil {
		t.Fatal(err)
	}
	want := batch.CallGraph().Snapshot()
	calls := 0
	adapter := FactAnalyzer{Analyzer: factAdapterAnalyzer{descriptor: analyzer.Descriptor{Name: "invoked-mutator"}, run: func(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
		calls++
		if len(input.Invoked) != 1 || len(input.Scripts) != 0 {
			t.Fatalf("invoked script was not selected: %+v", input)
		}
		input.Invoked[0].Text = "changed"
		input.Invoked[0].Occurrence.Ancestry[0] = "changed"
		input.Invoked[0].Spans[0].End = 999
		input.SelectedScripts[0] = "changed"
		input.Available[0] = analyzer.Source
		return nil, nil
	}}}
	if _, err := adapter.AnalyzeScripts(t.Context(), batch); err != nil || calls != len(batch.Scripts()) {
		t.Fatalf("invoked analysis failed: calls=%d error=%v", calls, err)
	}
	if !reflect.DeepEqual(batch.CallGraph().Snapshot(), want) {
		t.Fatal("invoked analyzer mutated batch graph")
	}
	for index := range batch.Scripts() {
		fact, exists := batch.Fact(index, false)
		if !exists || fact.Text == "changed" || fact.Spans[0].End == 999 || fact.Occurrence.Ancestry[0] != want.Entry.ID {
			t.Fatal("invoked analyzer mutated parent script facts")
		}
	}
}

func TestFactAnalyzerRejectsUnavailablePreparedFact(t *testing.T) {
	batch := twoRunFactBatch(t)
	corrupted := *batch.data
	corrupted.scripts = slices.Clone(batch.data.scripts)
	corrupted.scripts[0].Occurrence = "forged"
	calls := 0
	adapter := FactAnalyzer{Analyzer: factAdapterAnalyzer{descriptor: analyzer.Descriptor{Name: "never"}, run: func(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
		calls++
		return nil, nil
	}}}
	if _, err := adapter.AnalyzeScripts(t.Context(), Batch{data: &corrupted}); !errors.Is(err, ErrInvalidBatch) || calls != 0 {
		t.Fatalf("unavailable fact reached analyzer: calls=%d error=%v", calls, err)
	}
}

func TestFactAnalyzerPreservesForeignDiagnosticPath(t *testing.T) {
	graph, collection, _, _ := actionBatchFixture(t, false)
	batch, err := NewBatch(graph, collection)
	if err != nil {
		t.Fatal(err)
	}
	adapter := FactAnalyzer{Analyzer: factAdapterAnalyzer{descriptor: analyzer.Descriptor{Name: "foreign"}, run: func(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
		return []diagnostic.Diagnostic{{Path: "sourced/helper.sh", Message: "foreign", Snippet: "captured foreign snippet"}}, nil
	}}}
	findings, err := adapter.AnalyzeScripts(t.Context(), batch)
	if err != nil || len(findings) != len(batch.Scripts()) {
		t.Fatalf("foreign findings lost: %v %v", findings, err)
	}
	for _, finding := range findings {
		if finding.Path != "sourced/helper.sh" || finding.Source != nil || finding.Snippet != "captured foreign snippet" || finding.Owner == nil || finding.IgnoreOrigin == nil {
			t.Fatalf("unit provenance overwrote foreign diagnostic source: %+v", finding)
		}
	}
}
