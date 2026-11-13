package checking_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/results"
)

type failedActionFactProbe struct {
	name  string
	fact  rulemeta.InputKind
	calls *atomic.Int32
}

func (probe failedActionFactProbe) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: probe.name, Category: "correctness", RequiredInputs: []rulemeta.InputKind{probe.fact}}
}

func (probe failedActionFactProbe) CheckDocument(_ context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	probe.calls.Add(1)
	if len(input.Scripts)+len(input.ActionInputs)+len(input.Invoked) != 0 || len(input.AvailableInputs) != 1 || input.AvailableInputs[0] != rulemeta.Source {
		return nil, errors.New("failed action leaked executable analyzer facts")
	}
	return nil, nil
}

func TestActionRecoveredDeclarationsDoNotEnableScriptAnalysis(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	var sourceCalls, scriptCalls atomic.Int32
	source := []byte("name: recovered\ninputs: invalid\nruns: {using: composite, steps: [{run: echo $HI, shell: bash}]}\n")
	result, err := checking.Analyze(t.Context(), checking.Options{
		WorkingDir: t.TempDir(),
		Analyzers: []checkinputs.Registration{
			{Checker: failedActionFactProbe{name: "failed-action-source", fact: rulemeta.Source, calls: &sourceCalls}},
			{Checker: failedActionFactProbe{name: "failed-action-scripts", fact: rulemeta.Scripts, calls: &scriptCalls}},
		},
		Tools: registry.Registrations(registry.Selections{"shellcheck": {Runner: func([]string, string) ([]byte, error) {
			calls.Add(1)
			return []byte(`{"comments":[]}`), nil
		}}}),
	}, []workspace.Source{{Path: "action.yml", Content: source}})
	if result == nil || err == nil || err.Error() != `analyzer "failed-action-scripts" requires unavailable input kind "scripts"` {
		t.Fatal("failed action lost its result or explicit unavailable input error", err)
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
