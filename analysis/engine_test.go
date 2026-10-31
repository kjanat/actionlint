package analysis_test

import (
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analysis"
	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/plugin"
)

type ruffEchoBackend struct{ *plugin.Engine }

func (b ruffEchoBackend) ScriptDescriptor() analyzer.ScriptEngineDescriptor {
	descriptor := b.Engine.ScriptDescriptor()
	descriptor.Engine = "ruff"
	descriptor.Rule.Name = "ruff"
	return descriptor
}

func TestScriptEngineBackendChecksPythonSteps(t *testing.T) {
	engine, err := plugin.LoadEngine(t.Context(), filepath.Join("..", "internal", "plugins", "core", "testdata", "engines", "echo.wasm"), plugin.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(t.Context())
	result, err := analysis.Analyze(t.Context(), analysis.Request{
		Sources: []analysis.Source{{Path: "ci.yml", Content: []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(missing)\n        shell: python\n")}},
		Options: analysis.Options{Tools: map[string]analysis.Tool{"ruff": {Backend: ruffEchoBackend{engine}}}},
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
