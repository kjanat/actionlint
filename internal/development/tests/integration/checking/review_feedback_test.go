package checking_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/checkrequest"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/results"
)

type namedFindingAnalyzer string

func (a namedFindingAnalyzer) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: string(a), Category: "correctness"}
}

func (namedFindingAnalyzer) AnalyzeDocument(context.Context, checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
	return []results.Diagnostic{{Message: "same observation", Start: sourcetext.Position{Line: 6, Column: 14}}}, nil
}

func TestIndependentAnalyzerFindingsAndInferredEnds(t *testing.T) {
	t.Parallel()
	result, err := checking.Analyze(t.Context(),

		checking.Options{Analyzers: []results.AnalyzerRegistration{
			{Analyzer: namedFindingAnalyzer("first")}, {Analyzer: namedFindingAnalyzer("second")},
		}}, []workspace.InputFile{{Path: "workflow.yml", Content: []byte(workflow)}},
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
		Analyzers:         []results.AnalyzerRegistration{{Analyzer: namedFindingAnalyzer("observed")}},
		SkipProjectConfig: true,
	}
	baseline, err := checking.Check(t.Context(), options, checkrequest.Request{Kind: checkrequest.Files, Paths: []string{path}})
	if err != nil || baseline == nil || len(baseline.Diagnostics) != 1 {
		t.Fatalf("baseline=%+v err=%v", baseline, err)
	}
	options.ConfigOverlays = []configuration.Overlay{overlay}
	result, err := checking.Check(t.Context(), options, checkrequest.Request{Kind: checkrequest.Files, Paths: []string{path}})
	if err != nil || result == nil || len(result.Diagnostics) != 0 {
		t.Fatalf("overlay did not disable lint findings: result=%+v err=%v", result, err)
	}
}
