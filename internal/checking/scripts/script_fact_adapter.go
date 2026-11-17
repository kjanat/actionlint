package scripts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"

	checkexecutor "actionlint.kjanat.dev/v2/internal/checking/executor"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/inputs/validation"
	bindingmodel "actionlint.kjanat.dev/v2/internal/model/binding"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

type FactAnalyzer struct {
	Analyzer         checkinputs.Checker
	Options          json.RawMessage
	IncludeExecution bool
}

type describedFactChecker struct {
	checkinputs.Checker
	descriptor checkinputs.Descriptor
}

func (c describedFactChecker) Descriptor() checkinputs.Descriptor { return c.descriptor }

func (adapter FactAnalyzer) AnalyzeScripts(ctx context.Context, batch Batch) ([]*findingmodel.Finding, error) {
	if !batch.Valid() {
		return nil, ErrInvalidBatch
	}
	available := []rulemeta.InputKind{rulemeta.Scripts, rulemeta.Invocations}
	if adapter.IncludeExecution {
		available = append(available, rulemeta.Execution)
	}
	if validation.IsNil(adapter.Analyzer) {
		return nil, errors.New("nil analyzer registration")
	}
	descriptor := adapter.Analyzer.Descriptor()
	checker := describedFactChecker{Checker: adapter.Analyzer, descriptor: descriptor}
	prepared := make(map[bindingmodel.ID]checkexecutor.PreparedRegistration)
	for _, scope := range batch.Scopes() {
		var identity bindingmodel.ID
		options := adapter.Options
		if scope.Bindings != nil {
			decision := scope.Bindings.Rule(descriptor.Name)
			identity = decision.ID()
			var err error
			options, err = decision.Options(adapter.Options)
			if err != nil {
				return nil, err
			}
		}
		if _, exists := prepared[identity]; exists {
			continue
		}
		registration, err := checkexecutor.Prepare(checkinputs.Registration{Checker: checker, Options: options}, available)
		if err != nil {
			return nil, err
		}
		prepared[identity] = registration
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	graph := batch.Invocations()
	var findings []*findingmodel.Finding
	var failures []error
	for index, unit := range batch.Scripts() {
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		if unit.Input != nil {
			continue
		}
		script, exists := batch.Fact(index, adapter.IncludeExecution)
		if !exists {
			failures = append(failures, fmt.Errorf("%w: script fact %d is unavailable", ErrInvalidBatch, index))
			break
		}
		input := checkinputs.Input{
			Revision: unit.Revision, Coordinates: unit.Coordinates,
			Reference: graph.Entry().Reference, CallGraph: graph, Path: unit.Path,
			AvailableInputs: available,
			SelectedScripts: []invocation.ID{script.Occurrence.ID},
		}
		if script.Occurrence.Owner == graph.RootID() {
			input.Scripts = []checkinputs.Script{script}
		} else {
			input.Invoked = []checkinputs.Script{script}
		}
		var identity bindingmodel.ID
		if scope := unit.Scope.Bindings; scope != nil {
			identity = scope.Rule(descriptor.Name).ID()
		}
		selected := prepared[identity]
		diagnostics, err := checkexecutor.RunOne(ctx, input, selected)
		for _, d := range diagnostics {
			finding := findingmodel.FromDiagnostic(d)
			if unit.Scope.Bindings != nil {
				finding.Binding = unit.Scope.Bindings.Rule(finding.Rule).ID()
			}
			if d.Path == unit.Path {
				finding.Source.Content = unit.Content
			}
			finding.IgnoreOrigin, finding.Owner = unit.Origin, unit.Owner
			findings = append(findings, finding)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("script %q (%s): %w", unit.Path, script.Occurrence.ID, err))
		}
		if canceled := ctx.Err(); canceled != nil {
			if !errors.Is(err, canceled) {
				failures = append(failures, canceled)
			}
			break
		}
	}
	return findings, errors.Join(failures...)
}
