package actionlintarch

import (
	"testing"

	"actionlint.kjanat.dev/v2/analyzers/architecture"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestArchitecture(t *testing.T) {
	contract, err := architecture.Load("../../architecture.json")
	if err != nil {
		t.Fatal(err)
	}
	packages := []string{
		"internal/cli",
		"internal/engine",
		"internal/expression",
		"internal/expression/context/generate",
		"internal/expression/newcheck",
		"internal/expressionish",
		"internal/semantics/newrule",
		"internal/sourceid",
		"internal/tools/execution",
		"internal/tools/shellcheck",
		"internal/undecided",
		"newdomain",
	}
	for i := range packages {
		packages[i] = module + "/" + packages[i]
	}
	analysistest.Run(t, analysistest.TestData(), New(contract), packages...)
}
