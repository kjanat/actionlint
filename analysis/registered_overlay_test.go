package analysis_test

import (
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/analysis"
	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/config"
)

func TestPublicOverlayAcceptsRegisteredAnalyzerSelectors(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "ci.yml")
	if err := os.WriteFile(path, []byte(workflow), 0o600); err != nil {
		t.Fatal(err)
	}
	options := analysis.FileOptions{
		WorkingDir:        root,
		Analyzers:         []analyzer.Registration{{Analyzer: namedFindingAnalyzer("observed")}},
		SkipProjectConfig: true,
	}
	baseline, err := analysis.Files(t.Context(), []string{path}, options)
	if err != nil || len(baseline.Diagnostics) != 1 {
		t.Fatalf("baseline=%+v err=%v", baseline, err)
	}
	for _, source := range []string{
		"ignores: [{rule: observed, file: '**'}]",
		"lint: {rules: {disable: [observed]}}",
	} {
		if _, err := config.ParseOverlay("config", []byte(source)); err == nil {
			t.Fatal("unregistered selector accepted")
		}
		overlay, err := config.ParseOverlay("config", []byte(source), "observed")
		if err != nil {
			t.Fatal(err)
		}
		options.ConfigOverlays = []config.Overlay{overlay}
		result, err := analysis.Files(t.Context(), []string{path}, options)
		if err != nil || len(result.Diagnostics) != 0 {
			t.Fatalf("%s: result=%+v err=%v", source, result, err)
		}
	}
}
