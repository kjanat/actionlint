package scripts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	checkexecutor "actionlint.kjanat.dev/v2/internal/checking/executor"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	diagmodel "actionlint.kjanat.dev/v2/internal/results"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

type FactAnalyzer struct {
	Analyzer checkinputs.Checker
	Options  json.RawMessage
}

func (adapter FactAnalyzer) AnalyzeScripts(ctx context.Context, batch Batch) ([]*findingmodel.Finding, error) {
	if !batch.Valid() {
		return nil, ErrInvalidBatch
	}
	available := []checkinputs.InputKind{checkinputs.Scripts, checkinputs.Invocations}
	prepared, err := checkexecutor.Prepare(checkinputs.Registration{Checker: adapter.Analyzer, Options: adapter.Options}, available)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	graph := batch.CallGraph()
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
		script, exists := batch.Fact(index, false)
		if !exists {
			failures = append(failures, fmt.Errorf("%w: script fact %d is unavailable", ErrInvalidBatch, index))
			break
		}
		input := checkinputs.Input{
			Revision: unit.Revision, Coordinates: unit.Coordinates,
			Reference: graph.Entry().Reference, CallGraph: graph, Path: unit.Path,
			AvailableInputs: available,
			SelectedScripts: []checkinputs.OccurrenceID{script.Occurrence.ID},
		}
		if script.Occurrence.Owner == graph.Entry().ID {
			input.Scripts = []checkinputs.Script{script}
		} else {
			input.Invoked = []checkinputs.Script{script}
		}
		diagnostics, err := checkexecutor.RunOne(ctx, input, prepared)
		for _, d := range diagnostics {
			finding := findingmodel.FromDiagnostic(d)
			if d.End == (diagmodel.Position{}) {
				finding.EndPosition = nil
			}
			if d.Path == unit.Path {
				finding.Source = unit.Content
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
