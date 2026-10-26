package architecture_test

import (
	"actionlint.kjanat.dev/v2/analyzers/architecture"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
	"testing"
)

func TestStructuralRules(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), architecture.Analyzer, "sample/api", "sample/helper", "sample/internal/detail")
}

func TestTypesOnlyPass(t *testing.T) {
	// golangci-lint can schedule a package without syntax. Exporting a fact
	// there panics in its driver; the analyzer must leave it unsummarized.
	_, err := architecture.Analyzer.Run(&analysis.Pass{
		ExportPackageFact: func(analysis.Fact) { t.Fatal("exported fact without source") },
	})
	if err != nil {
		t.Fatal(err)
	}
}
