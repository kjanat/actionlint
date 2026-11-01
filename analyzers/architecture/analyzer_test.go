package architecture_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/analyzers/architecture"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestStructuralRules(t *testing.T) {
	strict := architecture.New()
	for _, flag := range []string{"strict-state", "strict-borrows"} {
		if err := strict.Flags.Set(flag, "true"); err != nil {
			t.Fatal(err)
		}
	}
	analysistest.Run(t, analysistest.TestData(), strict, "sample/api", "sample/helper", "sample/internal/detail")
}

func TestDefaultPolicy(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), architecture.New(), "defaultpolicy/api", "defaultpolicy/internal/detail", "defaultpolicy/internal/state")
}

func TestStrictStatePolicy(t *testing.T) {
	strict := architecture.New()
	if err := strict.Flags.Set("strict-state", "true"); err != nil {
		t.Fatal(err)
	}
	analysistest.Run(t, analysistest.TestData(), strict, "strictpolicy/internal/state")
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
