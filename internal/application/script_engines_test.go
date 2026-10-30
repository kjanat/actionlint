package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
)

type recordingEngine struct {
	mu       sync.Mutex
	requests []analyzer.ScriptRequest
	closed   bool
	code     string
}

func (e *recordingEngine) Check(_ context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	e.mu.Lock()
	e.requests = append(e.requests, request)
	e.mu.Unlock()
	code := e.code
	if code == "" {
		code = "F821"
	}
	row, column := 1, 7
	if code == "SC2086" {
		before, _, _ := strings.Cut(request.Source, "missing")
		row = strings.Count(before, "\n") + 1
		column = len(before) - strings.LastIndex(before, "\n")
	}
	return []analyzer.ScriptDiagnostic{{
		Filename: request.Filename, Code: code, Message: "Undefined name `missing`", Severity: "error",
		Location: analyzer.ScriptPosition{Row: row, Column: column}, EndLocation: analyzer.ScriptPosition{Row: row, Column: column + 7},
	}}, nil
}

func (e *recordingEngine) ValidateConfig(context.Context, json.RawMessage) error { return nil }

func (e *recordingEngine) Close(context.Context) error {
	e.closed = true
	return nil
}

func writeScriptEngineRepository(t *testing.T) (root string, workflows []string) {
	t.Helper()
	root = t.TempDir()
	for path, content := range map[string]string{
		".git/HEAD":                   "ref: refs/heads/main\n",
		".github/actionlint.yaml":     "tools:\n  ruff:\n    backend:\n      wasm: {path: engines/ruff.wasm}\n",
		".github/workflows/ci.yml":    "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(missing)\n        shell: python\n",
		".github/workflows/night.yml": "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(missing)\n        shell: python\n",
	} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root, []string{filepath.Join(root, ".github", "workflows", "ci.yml"), filepath.Join(root, ".github", "workflows", "night.yml")}
}

func TestConfiguredScriptEngineLoadsOncePerInvocation(t *testing.T) {
	root, workflows := writeScriptEngineRepository(t)
	engine := &recordingEngine{}
	var paths []string
	session := application.Options{WorkingDir: root, LoadScriptEngine: func(_ context.Context, path string) (analyzer.ScriptBackend, error) {
		paths = append(paths, path)
		return engine, nil
	}, Tools: registry.Registrations(registry.Selections{"ruff": {Command: "ruff", Process: &processconfig.Options{Optional: true}}})}

	result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: workflows})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != filepath.Join(root, ".github", "engines", "ruff.wasm") {
		t.Fatalf("loaded paths=%q", paths)
	}
	if len(result.Diagnostics) != 2 || len(engine.requests) != 2 || engine.requests[0].Source != "print(missing)" {
		t.Fatalf("diagnostics=%+v requests=%+v", result.Diagnostics, engine.requests)
	}
	finding := result.Diagnostics[0]
	if finding.Rule != "ruff" || finding.Code != "F821" || finding.Start.Line != 6 || finding.Start.Column != 20 {
		t.Fatalf("finding=%+v", finding)
	}
	if !engine.closed {
		t.Fatal("invocation returned without closing its loaded engine")
	}
}

func TestOverlayScriptEnginePathsUseEachSessionWorkingDirectory(t *testing.T) {
	overlay, err := config.ParseOverlay("config", []byte("tools:\n  ruff:\n    backend:\n      wasm: {path: engines/overlay.wasm}\noverrides:\n  - includes: ['**/night.yml']\n    tools:\n      ruff:\n        backend:\n          wasm: {path: engines/night.wasm}\n"))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		root, workflows := writeScriptEngineRepository(t)
		loaded := map[string]bool{}
		var loadedMu sync.Mutex
		options := application.Options{
			WorkingDir: root, ConfigOverlays: []config.Overlay{overlay},
			Tools: registry.Registrations(registry.Selections{"ruff": {Command: "ruff", Process: &processconfig.Options{Optional: true}}}),
			LoadScriptEngine: func(_ context.Context, path string) (analyzer.ScriptBackend, error) {
				loadedMu.Lock()
				loaded[path] = true
				loadedMu.Unlock()
				return &recordingEngine{}, nil
			},
		}
		result, err := application.Check(t.Context(), options, application.Input{Kind: application.Files, Paths: workflows})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Diagnostics) != 2 || len(loaded) != 2 || !loaded[filepath.Join(root, "engines", "overlay.wasm")] || !loaded[filepath.Join(root, "engines", "night.wasm")] {
			t.Fatalf("root=%s loaded=%v diagnostics=%+v", root, loaded, result.Diagnostics)
		}
	}
}

func TestConfiguredScriptEngineFailsWithoutLoader(t *testing.T) {
	root, workflows := writeScriptEngineRepository(t)
	session := application.Options{WorkingDir: root, Tools: registry.Registrations(registry.Selections{"ruff": {Command: "ruff", Process: &processconfig.Options{Optional: true}}})}

	if _, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: workflows[:1]}); err == nil || !strings.Contains(err.Error(), "tools.ruff.backend.wasm selects "+filepath.Join(root, ".github", "engines", "ruff.wasm")+", and this host has no WebAssembly engine loader") {
		t.Fatalf("err=%v", err)
	}
	failing := errors.New("module is broken")
	session = application.Options{WorkingDir: root, LoadScriptEngine: func(context.Context, string) (analyzer.ScriptBackend, error) { return nil, failing }, Tools: registry.Registrations(registry.Selections{"ruff": {Command: "ruff", Process: &processconfig.Options{Optional: true}}})}

	if _, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: workflows[:1]}); !errors.Is(err, failing) || !strings.Contains(err.Error(), "could not initialize ruff: module is broken") {
		t.Fatalf("err=%v", err)
	}
}

func TestConfiguredScriptEngineRejectsNilLoaderBackend(t *testing.T) {
	root, workflows := writeScriptEngineRepository(t)
	options := application.Options{WorkingDir: root,
		Tools:            registry.Registrations(registry.Selections{"ruff": {Command: "ruff", Process: &processconfig.Options{Optional: true}}}),
		LoadScriptEngine: func(context.Context, string) (analyzer.ScriptBackend, error) { return nil, nil },
	}
	if _, err := application.Check(t.Context(), options, application.Input{Kind: application.Files, Paths: workflows[:1]}); err == nil || !strings.Contains(err.Error(), "could not initialize ruff") || !strings.Contains(err.Error(), "nil backend") {
		t.Fatalf("configured Wasm analysis silently fell back: %v", err)
	}
}

func TestExplicitToolSettingsWinOverConfiguredEngine(t *testing.T) {
	root, workflows := writeScriptEngineRepository(t)
	loads := 0
	loader := func(context.Context, string) (analyzer.ScriptBackend, error) {
		loads++
		return &recordingEngine{}, nil
	}
	disabled := application.Options{WorkingDir: root, LoadScriptEngine: loader, Tools: registry.Registrations(registry.Selections{"ruff": {Command: ""}})}

	if result, err := application.Check(t.Context(), disabled, application.Input{Kind: application.Files, Paths: workflows[:1]}); err != nil || len(result.Diagnostics) != 0 || loads != 0 {
		t.Fatalf("disabled: result=%+v err=%v loads=%d", result, err, loads)
	}
	injected := &recordingEngine{}
	explicit := application.Options{WorkingDir: root, LoadScriptEngine: loader, Tools: registry.Registrations(registry.Selections{"ruff": {Backend: injected}})}

	if result, err := application.Check(t.Context(), explicit, application.Input{Kind: application.Files, Paths: workflows[:1]}); err != nil || len(result.Diagnostics) != 1 || loads != 0 || len(injected.requests) != 1 {
		t.Fatalf("explicit: result=%+v err=%v loads=%d", result, err, loads)
	}
}

type namedShellEngine string

func (e namedShellEngine) Check(_ context.Context, request analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	row := strings.Count(strings.TrimSuffix(request.Source, "\n"), "\n") + 1
	return []analyzer.ScriptDiagnostic{{Filename: request.Filename, Code: "SC2086", Message: string(e), Severity: "warning", Location: analyzer.ScriptPosition{Row: row, Column: 1}, EndLocation: analyzer.ScriptPosition{Row: row, Column: 5}}}, nil
}

func TestShellcheckBackendFollowsCompositeConfiguration(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		".git/HEAD":                        "ref: refs/heads/main\n",
		".github/actionlint.yaml":          "tools:\n  shellcheck:\n    backend:\n      wasm: {path: workflow.wasm}\noverrides:\n  - includes: ['.github/actions/**/action.yml']\n    tools:\n      shellcheck:\n        backend:\n          wasm: {path: action.wasm}\n",
		".github/workflows/ci.yml":         "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./.github/actions/setup\n      - run: echo workflow\n",
		".github/actions/setup/action.yml": "name: setup\ndescription: setup\nruns:\n  using: composite\n  steps:\n    - run: echo action\n      shell: bash\n",
	}
	for path, content := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range []string{".github/workflows/ci.yml", ".github/actions/setup/action.yml"} {
		session := application.Options{WorkingDir: root, LoadScriptEngine: func(_ context.Context, path string) (analyzer.ScriptBackend, error) {
			return namedShellEngine(filepath.Base(path)), nil
		}, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: "shellcheck", Process: &processconfig.Options{Optional: true}}})}
		var err error

		result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{filepath.Join(root, filepath.FromSlash(input))}})
		if err != nil {
			t.Fatal(err)
		}
		foundAction := false
		for _, d := range result.Diagnostics {
			if strings.HasSuffix(filepath.ToSlash(d.Path), "action.yml") {
				foundAction = true
				if !strings.Contains(d.Message, "action.wasm") {
					t.Fatalf("%s: action used workflow backend: %+v", input, d)
				}
			} else if !strings.Contains(d.Message, "workflow.wasm") {
				t.Fatalf("unexpected workflow finding: %+v", d)
			}
		}
		if !foundAction {
			t.Fatalf("%s: missing composite finding: %+v", input, result.Diagnostics)
		}
	}
}

func TestShellcheckSelectsEngineForEachEffectiveConfiguration(t *testing.T) {
	root, workflows := writeScriptEngineRepository(t)
	config := "tools:\n  shellcheck:\n    backend:\n      wasm: {path: first.wasm}\noverrides:\n  - includes: ['**/night.yml']\n    tools:\n      shellcheck:\n        backend:\n          wasm: {path: second.wasm}\n"
	if err := os.WriteFile(filepath.Join(root, ".github", "actionlint.yaml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range workflows {
		if err := os.WriteFile(path, []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo $missing\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	loaded := map[string]*recordingEngine{}
	options := application.Options{
		WorkingDir: root, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: "shellcheck", Process: &processconfig.Options{Optional: true}}}),
		LoadScriptEngine: func(_ context.Context, path string) (analyzer.ScriptBackend, error) {
			mu.Lock()
			defer mu.Unlock()
			backend := &recordingEngine{code: "SC2086"}
			loaded[filepath.Base(path)] = backend
			return backend, nil
		},
	}
	result, err := application.Check(t.Context(), options, application.Input{Kind: application.Files, Paths: workflows})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 2 || len(loaded) != 2 {
		t.Fatalf("diagnostics=%+v engines=%v", result.Diagnostics, loaded)
	}
	for name, backend := range loaded {
		if len(backend.requests) != 1 {
			t.Fatalf("%s requests=%+v", name, backend.requests)
		}
	}
}
