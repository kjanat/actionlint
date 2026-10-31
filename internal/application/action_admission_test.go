package application_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

type failedActionFactProbe struct {
	name  string
	fact  analyzer.Fact
	calls *atomic.Int32
}

func (probe failedActionFactProbe) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: probe.name, Requires: []analyzer.Fact{probe.fact}}
}

func (probe failedActionFactProbe) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	probe.calls.Add(1)
	if len(input.Scripts)+len(input.ActionInputs)+len(input.Invoked) != 0 || len(input.Available) != 1 || input.Available[0] != analyzer.Source {
		return nil, errors.New("failed action leaked executable analyzer facts")
	}
	return nil, nil
}

func TestActionRecoveredDeclarationsDoNotEnableScriptAnalysis(t *testing.T) {
	var calls atomic.Int32
	var sourceCalls, scriptCalls atomic.Int32
	source := []byte("name: recovered\ninputs: invalid\nruns: {using: composite, steps: [{run: echo $HI, shell: bash}]}\n")
	result, err := application.Analyze(t.Context(), application.Options{
		WorkingDir: t.TempDir(),
		Analyzers: []analyzer.Registration{
			{Analyzer: failedActionFactProbe{name: "failed-action-source", fact: analyzer.Source, calls: &sourceCalls}},
			{Analyzer: failedActionFactProbe{name: "failed-action-scripts", fact: analyzer.Scripts, calls: &scriptCalls}},
		},
		Tools: registry.Registrations(registry.Selections{"shellcheck": {Runner: func([]string, string) ([]byte, error) {
			calls.Add(1)
			return []byte(`{"comments":[]}`), nil
		}}}),
	}, []workspace.Source{{Path: "action.yml", Content: source}})
	if result == nil || err == nil || err.Error() != `analyzer "failed-action-scripts" requires unavailable fact "scripts"` {
		t.Fatal("failed action lost its result or explicit unavailable-fact error", err)
	}
	if calls.Load() != 0 || scriptCalls.Load() != 0 || sourceCalls.Load() != 1 {
		t.Fatal("recovered source declarations reached script analysis")
	}
	var found bool
	for _, finding := range result.Diagnostics {
		found = found || strings.Contains(finding.Message, "inputs must be mapping")
	}
	if !found {
		t.Fatal("analysis discarded the action parse error", result.Diagnostics)
	}
}
