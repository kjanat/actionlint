package checking_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	workspace "actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/results"
)

type namedFindingAnalyzer string

func (a namedFindingAnalyzer) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: string(a), Category: "correctness"}
}

func (namedFindingAnalyzer) CheckDocument(context.Context, checkinputs.Input) ([]results.Diagnostic, error) {
	return []results.Diagnostic{{Message: "same observation", Start: sourcepos.Position{Line: 6, Column: 14}}}, nil
}

func TestIndependentAnalyzerFindingsAndInferredEnds(t *testing.T) {
	t.Parallel()
	result, err := checking.Analyze(t.Context(),

		checking.Options{Analyzers: []checkinputs.Registration{
			{Checker: namedFindingAnalyzer("first")}, {Checker: namedFindingAnalyzer("second")},
		}}, []workspace.Source{{Path: "workflow.yml", Content: []byte(workflow)}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("independent findings dropped: %+v", result.Diagnostics)
	}
	for _, finding := range result.Diagnostics {
		if finding.End.Line != finding.Start.Line || finding.End.Column <= finding.Start.Column {
			t.Errorf("missing end must produce an inferred nonempty range: %+v", finding)
		}
	}
}

func TestPublicOverlayChangesFileAnalysis(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "ci.yml")
	if err := os.WriteFile(path, []byte(workflow), 0o600); err != nil {
		t.Fatal(err)
	}
	overlay, err := checking.ConfigurationRules().ParseOverlay("lint", []byte("{enabled: false}"))
	if err != nil {
		t.Fatal(err)
	}
	options := checking.Options{
		WorkingDir:        root,
		Analyzers:         []checkinputs.Registration{{Checker: namedFindingAnalyzer("observed")}},
		SkipProjectConfig: true,
	}
	baseline, err := checking.Check(t.Context(), options, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{path}})
	if err != nil || baseline == nil || len(baseline.Diagnostics) != 1 {
		t.Fatalf("baseline=%+v err=%v", baseline, err)
	}
	options.ConfigOverlays = []config.Overlay{overlay}
	result, err := checking.Check(t.Context(), options, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{path}})
	if err != nil || result == nil || len(result.Diagnostics) != 0 {
		t.Fatalf("overlay did not disable lint findings: result=%+v err=%v", result, err)
	}
}
