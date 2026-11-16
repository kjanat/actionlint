package checking_test

import (
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/registry"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/module"
)

type ruffEchoBackend struct{ *module.Engine }

func (b ruffEchoBackend) ScriptDescriptor() checkinputs.ScriptEngineDescriptor {
	descriptor := b.Engine.ScriptDescriptor()
	descriptor.Engine = "ruff"
	descriptor.Rule.Name = "ruff"
	return descriptor
}

func TestScriptEngineBackendChecksPythonSteps(t *testing.T) {
	t.Parallel()
	engine, err := module.LoadModuleEngine(t.Context(), filepath.Join("..", "..", "..", "..", "plugins", "pluginexecution", "module", "testdata", "engines", "echo.wasm"), module.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(t.Context())
	result, err := checking.Analyze(t.Context(),

		checking.Options{Tools: registry.Registrations(registry.ToolSelections{"ruff": {Backend: ruffEchoBackend{engine}}})}, []workspace.InputFile{{Path: "ci.yml", Content: []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(missing)\n        shell: python\npermissions: {}\n")}},
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
