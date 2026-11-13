package checking_test

import (
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	registry "actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	workspace "actionlint.kjanat.dev/v2/internal/input/workspace"
	corewasm "actionlint.kjanat.dev/v2/internal/plugins/execution/module"
)

type ruffEchoBackend struct{ *corewasm.Engine }

func (b ruffEchoBackend) ScriptDescriptor() checkinputs.ScriptEngineDescriptor {
	descriptor := b.Engine.ScriptDescriptor()
	descriptor.Engine = "ruff"
	descriptor.Rule.Name = "ruff"
	return descriptor
}

func TestScriptEngineBackendChecksPythonSteps(t *testing.T) {
	t.Parallel()
	engine, err := corewasm.Load(t.Context(), filepath.Join("..", "..", "..", "..", "plugins", "execution", "module", "testdata", "engines", "echo.wasm"), corewasm.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(t.Context())
	result, err := checking.Analyze(t.Context(),

		checking.Options{Tools: registry.Registrations(registry.Selections{"ruff": {Backend: ruffEchoBackend{engine}}})}, []workspace.Source{{Path: "ci.yml", Content: []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(missing)\n        shell: python\npermissions: {}\n")}},
	)
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
