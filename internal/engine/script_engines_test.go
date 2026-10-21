package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"actionlint.kjanat.dev/analyzer"
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

func TestConfiguredScriptEngineLoadsOncePerSession(t *testing.T) {
	root, workflows := writeScriptEngineRepository(t)
	engine := &recordingEngine{}
	var paths []string
	session, err := NewAnalysisSession(AnalysisOptions{
		Context: t.Context(), WorkingDir: root, Ruff: "ruff", RuffOptions: &ExternalCommandOptions{Optional: true}, QuietSelection: true,
		LoadScriptEngine: func(_ context.Context, path string) (analyzer.ScriptBackend, error) {
			paths = append(paths, path)
			return engine, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Files(workflows, nil)
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
	if engine.closed {
		t.Fatal("engine closed before the session")
	}
	if err := session.Close(t.Context()); err != nil || !engine.closed {
		t.Fatalf("close err=%v closed=%v", err, engine.closed)
	}
}

func TestConfiguredScriptEngineFailsWithoutLoader(t *testing.T) {
	root, workflows := writeScriptEngineRepository(t)
	session, err := NewAnalysisSession(AnalysisOptions{Context: t.Context(), WorkingDir: root, Ruff: "ruff", RuffOptions: &ExternalCommandOptions{Optional: true}, QuietSelection: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Files(workflows[:1], nil); err == nil || !strings.Contains(err.Error(), "tools.ruff.backend.wasm selects "+filepath.Join(root, ".github", "engines", "ruff.wasm")+", and this host has no WebAssembly engine loader") {
		t.Fatalf("err=%v", err)
	}
	failing := errors.New("module is broken")
	session, err = NewAnalysisSession(AnalysisOptions{
		Context: t.Context(), WorkingDir: root, Ruff: "ruff", RuffOptions: &ExternalCommandOptions{Optional: true}, QuietSelection: true,
		LoadScriptEngine: func(context.Context, string) (analyzer.ScriptBackend, error) { return nil, failing },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Files(workflows[:1], nil); !errors.Is(err, failing) || !strings.Contains(err.Error(), "could not initialize Ruff: module is broken") {
		t.Fatalf("err=%v", err)
	}
}

func TestExplicitToolSettingsWinOverConfiguredEngine(t *testing.T) {
	root, workflows := writeScriptEngineRepository(t)
	loads := 0
	loader := func(context.Context, string) (analyzer.ScriptBackend, error) {
		loads++
		return &recordingEngine{}, nil
	}
	disabled, err := NewAnalysisSession(AnalysisOptions{Context: t.Context(), WorkingDir: root, Ruff: "", QuietSelection: true, LoadScriptEngine: loader})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := disabled.Files(workflows[:1], nil); err != nil || len(result.Diagnostics) != 0 || loads != 0 {
		t.Fatalf("disabled: result=%+v err=%v loads=%d", result, err, loads)
	}
	injected := &recordingEngine{}
	explicit, err := NewAnalysisSession(AnalysisOptions{Context: t.Context(), WorkingDir: root, RuffBackend: injected, QuietSelection: true, LoadScriptEngine: loader})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := explicit.Files(workflows[:1], nil); err != nil || len(result.Diagnostics) != 1 || loads != 0 || len(injected.requests) != 1 {
		t.Fatalf("explicit: result=%+v err=%v loads=%d", result, err, loads)
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
	session, err := NewAnalysisSession(AnalysisOptions{
		Context: t.Context(), WorkingDir: root, Shellcheck: "shellcheck", ShellcheckOptions: &ExternalCommandOptions{Optional: true}, QuietSelection: true,
		LoadScriptEngine: func(_ context.Context, path string) (analyzer.ScriptBackend, error) {
			mu.Lock()
			defer mu.Unlock()
			backend := &recordingEngine{code: "SC2086"}
			loaded[filepath.Base(path)] = backend
			return backend, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(t.Context())
	result, err := session.Files(workflows, nil)
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
