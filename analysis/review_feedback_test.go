package analysis_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/analysis"
	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/config"
	"actionlint.kjanat.dev/diagnostic"
)

type namedFindingAnalyzer string

func (a namedFindingAnalyzer) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: string(a)}
}

func (namedFindingAnalyzer) Analyze(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
	return []diagnostic.Diagnostic{{Message: "same observation", Start: diagnostic.Position{Line: 6, Column: 14}}}, nil
}

func TestIndependentAnalyzerFindingsAndInferredEnds(t *testing.T) {
	result, err := analysis.Analyze(t.Context(), analysis.Request{
		Sources: []analysis.Source{{Path: "workflow.yml", Content: []byte(workflow)}},
		Options: analysis.Options{Analyzers: []analyzer.Registration{
			{Analyzer: namedFindingAnalyzer("first")}, {Analyzer: namedFindingAnalyzer("second")},
		}},
	})
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
	root := t.TempDir()
	path := filepath.Join(root, "ci.yml")
	if err := os.WriteFile(path, []byte(workflow), 0o600); err != nil {
		t.Fatal(err)
	}
	overlay, err := config.ParseOverlay("lint", []byte("{enabled: false}"))
	if err != nil {
		t.Fatal(err)
	}
	options := analysis.FileOptions{
		Options: analysis.Options{
			WorkingDir: root,
			Analyzers:  []analyzer.Registration{{Analyzer: namedFindingAnalyzer("observed")}},
		},
		SkipProjectConfig: true,
	}
	baseline, err := analysis.Files(t.Context(), []string{path}, options)
	if err != nil || baseline == nil || len(baseline.Diagnostics) != 1 {
		t.Fatalf("baseline=%+v err=%v", baseline, err)
	}
	options.ConfigOverlays = []config.Overlay{overlay}
	result, err := analysis.Files(t.Context(), []string{path}, options)
	if err != nil || result == nil || len(result.Diagnostics) != 0 {
		t.Fatalf("overlay did not disable lint findings: result=%+v err=%v", result, err)
	}
}
