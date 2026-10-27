package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/analyzer"
	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/analyzerhost"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
)

type FactAnalyzer struct {
	Analyzer analyzer.Analyzer
	Options  json.RawMessage
}

func (adapter FactAnalyzer) AnalyzeScripts(ctx context.Context, batch Batch) ([]*findingmodel.Finding, error) {
	if !batch.Valid() {
		return nil, ErrInvalidBatch
	}
	available := []analyzer.Fact{analyzer.Scripts, analyzer.Invocations}
	prepared, err := analyzerhost.Prepare(analyzer.Registration{Analyzer: adapter.Analyzer, Options: adapter.Options}, available)
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
		input := analyzer.Input{
			Reference: graph.Entry().Reference, CallGraph: graph, Path: unit.Path,
			Available:       available,
			SelectedScripts: []analyzer.OccurrenceID{script.Occurrence.ID},
		}
		if script.Occurrence.Owner == graph.Entry().ID {
			input.Scripts = []analyzer.Script{script}
		} else {
			input.Invoked = []analyzer.Script{script}
		}
		diagnostics, err := analyzerhost.RunOne(ctx, input, prepared)
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
