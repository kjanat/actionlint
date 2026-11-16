package managed

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	"actionlint.kjanat.dev/v2/internal/development/tests/support/invocationfixture"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
	"actionlint.kjanat.dev/v2/internal/results"
)

type fallbackGuest struct{ inputs []checkinputs.AnalyzerInput }

func (*fallbackGuest) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "shellcheck", Category: "correctness"}
}

func (*fallbackGuest) ToolIntegration() *checkinputs.ToolIntegration {
	return &checkinputs.ToolIntegration{Tool: "shellcheck"}
}

func (*fallbackGuest) NegotiateTool(_ context.Context, data, _ json.RawMessage) (*checkinputs.ToolNegotiation, error) {
	var options struct {
		RCFile string `json:"rc-file"`
	}
	if err := json.Unmarshal(data, &options); err != nil {
		return nil, err
	}
	if options.RCFile != "" {
		return nil, &checkinputs.UnsupportedToolConfigurationError{Reason: "rc file"}
	}
	return &checkinputs.ToolNegotiation{Options: data}, nil
}

func (guest *fallbackGuest) CheckDocument(_ context.Context, input checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
	guest.inputs = append(guest.inputs, input)
	return []results.Diagnostic{{Path: input.Path, Message: "plugin"}}, nil
}

type fallbackRunner func(context.Context, tools.Batch) ([]tools.ToolFinding, error)

func (run fallbackRunner) Analyze(ctx context.Context, batch tools.Batch) ([]tools.ToolFinding, error) {
	return run(ctx, batch)
}

func documentBatchFixture(tb testing.TB, count int) tools.Batch {
	tb.Helper()
	declared := make([]invocationfixture.Script, count)
	for index := range declared {
		declared[index] = invocationfixture.Script{Node: fmt.Sprintf("run%d", index), Entry: true}
	}
	batch := tools.Batch{CallGraph: invocationfixture.Graph(tb, declared...)}
	for _, occurrence := range batch.CallGraph.Scripts() {
		fact := checkinputs.Script{Reference: occurrence.Declaration, Text: "echo test", Occurrence: occurrence, Runtime: &scriptmodel.Execution{Shell: "bash", ShellKnown: true}}
		batch.Scripts = append(batch.Scripts, tools.Script{Fact: fact, Text: fact.Text, Path: "workflow.yml"})
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
	batch.Scripts[1].Config = cfg
	guest := &fallbackGuest{}
	nativeCalls := 0
	runner := documentRunner{documentRoute: shellcheckRoute, guest: guest, integrator: guest, context: tools.Context{Document: checkinputs.AnalyzerInput{Reference: entry, Path: "workflow.yml", Kind: "workflow", ParseStatus: "complete"}}, native: func(selected *configuration.Config) (tools.Runner, error) {
		if selected != cfg {
			t.Fatal("fallback initialized with the workflow configuration instead of the declined script configuration")
		}
		return fallbackRunner(func(_ context.Context, selected tools.Batch) ([]tools.ToolFinding, error) {
			nativeCalls++
			if len(selected.Scopes) != 0 || !reflect.DeepEqual(selected.CallGraph, graph) || len(selected.Scripts) != 1 || !reflect.DeepEqual(selected.Scripts[0].Fact, batch.Scripts[1].Fact) {
				t.Fatal("native fallback lost graph or script fact")
			}
			return []tools.ToolFinding{{Script: 0, Message: "native"}}, nil
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

func (*scalingGuest) CheckDocument(context.Context, checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
	return nil, nil
}

func BenchmarkDocumentPluginGraph(b *testing.B) {
	for _, count := range []int{64, 256} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			batch := documentBatchFixture(b, count)
			guest := &scalingGuest{}
			runner := documentRunner{documentRoute: shellcheckRoute, guest: guest, integrator: guest, context: tools.Context{Document: checkinputs.AnalyzerInput{Reference: batch.CallGraph.Entry().Reference, Path: "workflow.yml", Kind: "workflow", ParseStatus: "complete"}}}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := runner.Analyze(b.Context(), batch); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
