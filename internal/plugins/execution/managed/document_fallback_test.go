package managed

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"

	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/scripts"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/invocationfixture"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
	"actionlint.kjanat.dev/v2/internal/results"
)

type fallbackGuest struct{ inputs []inputs.AnalyzerInput }

func (*fallbackGuest) Descriptor() inputs.Descriptor {
	return inputs.Descriptor{Name: "shellcheck", Category: "correctness"}
}

func (*fallbackGuest) ToolIntegration() *inputs.ToolIntegration {
	return &inputs.ToolIntegration{Tool: "shellcheck"}
}

func (*fallbackGuest) NegotiateTool(_ context.Context, data, _ json.RawMessage) (*inputs.ToolNegotiation, error) {
	var options struct {
		RCFile string `json:"rc-file"`
	}
	if err := json.Unmarshal(data, &options); err != nil {
		return nil, err
	}
	if options.RCFile != "" {
		return nil, &inputs.UnsupportedToolConfigurationError{Reason: "rc file"}
	}
	return &inputs.ToolNegotiation{Options: data}, nil
}

func (guest *fallbackGuest) CheckDocument(_ context.Context, input inputs.AnalyzerInput) ([]results.Diagnostic, error) {
	guest.inputs = append(guest.inputs, input)
	return []results.Diagnostic{{Path: input.Path, Message: "plugin"}}, nil
}

type fallbackRunner func(context.Context, scripts.Batch) ([]contract.ToolFinding, error)

func (run fallbackRunner) Analyze(ctx context.Context, batch scripts.Batch) ([]contract.ToolFinding, error) {
	return run(ctx, batch)
}

func documentBatchFixture(tb testing.TB, count int) scripts.Batch {
	tb.Helper()
	declared := make([]invocationfixture.Script, count)
	for index := range declared {
		declared[index] = invocationfixture.Script{Node: fmt.Sprintf("run%d", index), Entry: true}
	}
	batch := scripts.Batch{CallGraph: invocationfixture.Graph(tb, declared...)}
	for _, occurrence := range batch.CallGraph.Scripts() {
		batch.Scripts = append(batch.Scripts, scripts.Unit{
			Occurrence: occurrence.ID, Reference: occurrence.Declaration, Path: "workflow.yml",
			Run:       &definition.ExecRun{Run: &definition.String{Value: "echo test"}},
			Execution: &scriptmodel.Execution{Shell: "bash", ShellKnown: true},
		})
	}
	return batch
}

func TestDocumentPluginMixedFallbackPreservesOccurrencesAndIndices(t *testing.T) {
	t.Parallel()
	batch := documentBatchFixture(t, 3)
	graph := batch.CallGraph
	entry := graph.Entry().Reference
	cfg, err := (configuration.RuleSet{}).ParseConfig([]byte("tools: {shellcheck: {config: .shellcheckrc}}"))
	if err != nil {
		t.Fatal(err)
	}
	batch.Scripts[1].Scope = &scripts.Scope{Config: cfg}
	guest := &fallbackGuest{}
	nativeCalls := 0
	runner := documentRunner{documentRoute: shellcheckRoute, guest: guest, integrator: guest, context: contract.Context{Document: inputs.AnalyzerInput{Reference: entry, Path: "workflow.yml", Kind: "workflow", ParseStatus: "complete"}}, native: func(selected *configuration.Config) (contract.Runner, error) {
		if selected != cfg {
			t.Fatal("fallback initialized with the workflow configuration instead of the declined script configuration")
		}
		return fallbackRunner(func(_ context.Context, selected scripts.Batch) ([]contract.ToolFinding, error) {
			nativeCalls++
			selectedFact, selectedExists := selected.Fact(0, true)
			declinedFact, declinedExists := batch.Fact(1, true)
			if len(selected.Scopes) != 1 || selected.Scopes[0] != batch.Scripts[1].Scope || !reflect.DeepEqual(selected.CallGraph, graph) || len(selected.Scripts) != 1 || !selectedExists || !declinedExists || !reflect.DeepEqual(selectedFact, declinedFact) {
				t.Fatal("native fallback lost graph or script fact")
			}
			return []contract.ToolFinding{{Script: 0, Message: "native"}}, nil
		}), nil
	}}
	findings, err := runner.Analyze(t.Context(), batch)
	if err != nil {
		t.Fatal(err)
	}
	if nativeCalls != 1 || len(guest.inputs) != 2 || len(findings) != 3 {
		t.Fatalf("lost mixed work: native=%d plugin=%d findings=%d", nativeCalls, len(guest.inputs), len(findings))
	}
	for index, want := range []int{0, 2} {
		input := guest.inputs[index]
		if err := input.ValidateInvocations(); err != nil {
			t.Fatal(err)
		}
		if len(input.SelectedScripts) != 1 || input.SelectedScripts[0] != graph.Scripts()[want].ID || findings[index].Script != want {
			t.Fatal("plugin selection or finding index changed")
		}
	}
	if findings[2].Script != 1 || findings[2].Message != "native" {
		t.Fatal("native index was not restored")
	}
}

type scalingGuest struct{ fallbackGuest }

func (*scalingGuest) CheckDocument(context.Context, inputs.AnalyzerInput) ([]results.Diagnostic, error) {
	return nil, nil
}

func BenchmarkDocumentPluginGraph(b *testing.B) {
	for _, count := range []int{64, 256} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			batch := documentBatchFixture(b, count)
			guest := &scalingGuest{}
			runner := documentRunner{documentRoute: shellcheckRoute, guest: guest, integrator: guest, context: contract.Context{Document: inputs.AnalyzerInput{Reference: batch.CallGraph.Entry().Reference, Path: "workflow.yml", Kind: "workflow", ParseStatus: "complete"}}}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := runner.Analyze(b.Context(), batch); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
