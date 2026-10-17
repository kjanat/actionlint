package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/tools/adapters"
)

type configurationEngine struct {
	validated []map[string]any
	checked   int
	failure   error
}

func (e *configurationEngine) ValidateConfig(_ context.Context, options json.RawMessage) error {
	var value map[string]any
	if err := json.Unmarshal(options, &value); err != nil {
		return err
	}
	e.validated = append(e.validated, value)
	return e.failure
}
func (e *configurationEngine) Check(context.Context, analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	e.checked++
	return nil, nil
}

func TestSelectedEngineOwnsConfigurationValidation(t *testing.T) {
	for _, tc := range []struct {
		name, settings string
		want           map[string]any
	}{
		{"preview selector", "config: {preview: true}, select: [E111]", map[string]any{"preview": true, "lint": map[string]any{"select": []any{"E111"}}}},
		{"engine version", "target-version: py399, select: [ENGINE999]", map[string]any{"target-version": "py399", "lint": map[string]any{"select": []any{"ENGINE999"}}}},
		{"JSON payload", "config: {engine-owned: [null]}", map[string]any{"engine-owned": []any{nil}}},
		{"engine defaults", "config: {}", map[string]any{}},
		{"engine read-only boundary", "config: {src: [virtual-root]}", map[string]any{"src": []any{"virtual-root"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, injected := range []bool{false, true} {
				t.Run(map[bool]string{false: "configured", true: "injected"}[injected], func(t *testing.T) {
					root, workflows := writeScriptEngineRepository(t)
					config := "tools: {ruff: {" + tc.settings
					if !injected {
						config += ", backend: {wasm: {path: engine.wasm}}"
					}
					config += "}}\n"
					if err := os.WriteFile(filepath.Join(root, ".github", "actionlint.yaml"), []byte(config), 0o600); err != nil {
						t.Fatal(err)
					}
					engine := &configurationEngine{}
					options := adapters.Options{Ruff: "ruff"}
					applicationOptions := application.Options{WorkingDir: root}
					if injected {
						options.Ruff = ""
						options.RuffBackend = engine
					} else {
						applicationOptions.LoadScriptEngine = func(context.Context, string) (analyzer.ScriptBackend, error) { return engine, nil }
					}
					applicationOptions.Tools = options.Registrations()
					result, err := application.Check(t.Context(), applicationOptions, application.Input{Kind: application.Files, Paths: workflows[:1]})
					if err != nil {
						t.Fatal(err)
					}
					if len(result.Diagnostics) != 0 || engine.checked != 1 || len(engine.validated) != 1 || !reflect.DeepEqual(engine.validated[0], tc.want) {
						t.Fatalf("config=%+v checks=%d diagnostics=%v", engine.validated, engine.checked, result.Diagnostics)
					}
				})
			}
		})
	}
}

func TestEngineConfigurationFailurePreventsCheck(t *testing.T) {
	root, workflows := writeScriptEngineRepository(t)
	failure := errors.New("engine rejected settings")
	engine := &configurationEngine{failure: failure}
	options := application.Options{WorkingDir: root, Tools: (adapters.Options{Ruff: "ruff"}).Registrations(), LoadScriptEngine: func(context.Context, string) (analyzer.ScriptBackend, error) { return engine, nil }}
	_, err := application.Check(t.Context(), options, application.Input{Kind: application.Files, Paths: workflows[:1]})
	if !errors.Is(err, failure) || len(engine.validated) != 1 || engine.checked != 0 {
		t.Fatalf("error=%v validations=%d checks=%d", err, len(engine.validated), engine.checked)
	}
}
