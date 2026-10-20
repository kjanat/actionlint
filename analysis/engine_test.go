package analysis_test

import (
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/analysis"
	"actionlint.kjanat.dev/plugin"
)

func TestScriptEngineBackendChecksPythonSteps(t *testing.T) {
	engine, err := plugin.LoadEngine(t.Context(), filepath.Join("..", "internal", "corewasm", "testdata", "engines", "echo.wasm"), plugin.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(t.Context())
	result, err := analysis.Analyze(t.Context(), analysis.Request{
		Sources: []analysis.Source{{Path: "ci.yml", Content: []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(missing)\n        shell: python\n")}},
		Options: analysis.Options{Ruff: analysis.Tool{Backend: engine}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("diagnostics=%+v", result.Diagnostics)
	}
	finding := result.Diagnostics[0]
	if finding.Rule != "ruff" || finding.Code != "F821" || finding.Start.Line != 6 || finding.Start.Column != 20 || !strings.Contains(finding.Message, "Undefined name `missing`") {
		t.Fatalf("finding=%+v", finding)
	}
}
