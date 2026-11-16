package checking_test

import (
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/checkrequest"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestPublicOverlayAcceptsRegisteredAnalyzerSelectors(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "ci.yml")
	if err := os.WriteFile(path, []byte(workflow), 0o600); err != nil {
		t.Fatal(err)
	}
	options := checking.Options{
		WorkingDir:        root,
		Analyzers:         []results.AnalyzerRegistration{{Analyzer: namedFindingAnalyzer("observed")}},
		SkipProjectConfig: true,
	}
	baseline, err := checking.Check(t.Context(), options, checkrequest.Request{Kind: checkrequest.Files, Paths: []string{path}})
	if err != nil || len(baseline.Diagnostics) != 1 {
		t.Fatalf("baseline=%+v err=%v", baseline, err)
	}
	for _, source := range []string{
		"ignores: [{rule: observed, file: '**'}]",
		"lint: {rules: {disable: [observed]}}",
	} {
		if _, err := checking.ConfigurationRules().ParseOverlay("config", []byte(source)); err == nil {
			t.Fatal("unregistered selector accepted")
		}
		overlay, err := checking.ConfigurationRules().ParseOverlay("config", []byte(source), "observed")
		if err != nil {
			t.Fatal(err)
		}
		options.ConfigOverlays = []configuration.Overlay{overlay}
		result, err := checking.Check(t.Context(), options, checkrequest.Request{Kind: checkrequest.Files, Paths: []string{path}})
		if err != nil || len(result.Diagnostics) != 0 {
			t.Fatalf("%s: result=%+v err=%v", source, result, err)
		}
	}
}
