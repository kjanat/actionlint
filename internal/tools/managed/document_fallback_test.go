package managed

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/source"
)

type fallbackGuest struct{ inputs []analyzer.Input }

func (*fallbackGuest) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "shellcheck"}
}
func (*fallbackGuest) ToolIntegration() *analyzer.ToolIntegration {
	return &analyzer.ToolIntegration{Tool: "shellcheck"}
}
func (*fallbackGuest) NegotiateTool(_ context.Context, data, _ json.RawMessage) (*analyzer.ToolNegotiation, error) {
	var options documentSettings
	if err := json.Unmarshal(data, &options); err != nil {
		return nil, err
	}
	if options.RCFile != "" {
		return nil, &analyzer.UnsupportedToolConfigurationError{Reason: "rc file"}
	}
	return &analyzer.ToolNegotiation{Options: data}, nil
}
func (guest *fallbackGuest) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	guest.inputs = append(guest.inputs, input)
	return []diagnostic.Diagnostic{{Path: input.Path, Message: "plugin"}}, nil
}

type fallbackRunner func(context.Context, tools.Batch) ([]tools.Finding, error)

func (run fallbackRunner) Analyze(ctx context.Context, batch tools.Batch) ([]tools.Finding, error) {
	return run(ctx, batch)
}

func documentBatchFixture(t testing.TB, count int) tools.Batch {
	t.Helper()
	entry := source.Reference{Document: "workflow.yml", Revision: "revision", Node: "document"}
	graphData := analyzer.CallGraphData{Entry: analyzer.InvocationEntry{ID: analyzer.OccurrenceID(fmt.Sprintf("%064x", 1)), Kind: analyzer.WorkflowEntry, Reference: entry}, Budget: analyzer.InvocationBudget{MaxDepth: 1, MaxCalls: 1, MaxScripts: count}, Complete: true}
	batch := tools.Batch{}
	for index := range count {
		declaration := entry
		declaration.Node = fmt.Sprintf("run%d", index)
		occurrence := analyzer.ScriptOccurrence{ID: analyzer.OccurrenceID(fmt.Sprintf("%064x", index+2)), Owner: graphData.Entry.ID, Ancestry: []analyzer.OccurrenceID{graphData.Entry.ID}, Declaration: declaration, Kind: analyzer.RunScript}
		graphData.Scripts = append(graphData.Scripts, occurrence)
		fact := analyzer.Script{Reference: declaration, Occurrence: occurrence, Text: "echo test", Runtime: &analyzer.Runtime{Shell: "bash", ShellKnown: true}}
		batch.Scripts = append(batch.Scripts, tools.Script{Fact: fact, Text: fact.Text, Path: "workflow.yml"})
	}
	graph, err := analyzer.NewCallGraph(graphData)
	if err != nil {
		t.Fatal(err)
	}
	batch.CallGraph = graph
	return batch
}

func TestDocumentPluginMixedFallbackPreservesOccurrencesAndIndices(t *testing.T) {
	batch := documentBatchFixture(t, 3)
	graph := batch.CallGraph
	entry := graph.Entry().Reference
	cfg, err := config.Parse([]byte("tools: {shellcheck: {config: .shellcheckrc}}"))
	if err != nil {
		t.Fatal(err)
	}
	batch.Scripts[1].Config = cfg
	guest := &fallbackGuest{}
	nativeCalls := 0
	runner := documentRunner{documentRoute: shellcheckRoute, guest: guest, integrator: guest, context: tools.Context{Document: analyzer.Input{Reference: entry, Path: "workflow.yml", Kind: "workflow", ParseStatus: "complete"}}, native: func() (tools.Runner, error) {
		return fallbackRunner(func(_ context.Context, selected tools.Batch) ([]tools.Finding, error) {
			nativeCalls++
			if len(selected.Scopes) != 0 || selected.CallGraph != graph || len(selected.Scripts) != 1 || !reflect.DeepEqual(selected.Scripts[0].Fact, batch.Scripts[1].Fact) {
				t.Fatal("native fallback lost graph or script fact")
			}
			return []tools.Finding{{Script: 0, Message: "native"}}, nil
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
		if len(input.SelectedScripts) != 1 || input.SelectedScripts[0] != graph.Snapshot().Scripts[want].ID || findings[index].Script != want {
			t.Fatal("plugin selection or finding index changed")
		}
	}
	if findings[2].Script != 1 || findings[2].Message != "native" {
		t.Fatal("native index was not restored")
	}
}

type scalingGuest struct{ fallbackGuest }

func (*scalingGuest) Analyze(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
	return nil, nil
}

func BenchmarkDocumentPluginGraph(b *testing.B) {
	for _, count := range []int{64, 256} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			batch := documentBatchFixture(b, count)
			guest := &scalingGuest{}
			runner := documentRunner{documentRoute: shellcheckRoute, guest: guest, integrator: guest, context: tools.Context{Document: analyzer.Input{Reference: batch.CallGraph.Entry().Reference, Path: "workflow.yml", Kind: "workflow", ParseStatus: "complete"}}}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := runner.Analyze(b.Context(), batch); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
