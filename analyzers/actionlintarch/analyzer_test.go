package actionlintarch

import (
	"golang.org/x/tools/go/analysis/analysistest"
	"testing"
)

func TestArchitecture(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer,
		module+"/internal/tools/shellcheck", module+"/internal/tools/execution", module+"/internal/engine",
		module+"/internal/expression", module+"/internal/cli", module+"/internal/sourceid")
}
