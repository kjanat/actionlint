package checking_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	registry "actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	workspace "actionlint.kjanat.dev/v2/internal/input/workspace"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
	scriptbackend "actionlint.kjanat.dev/v2/internal/plugins/execution/scripts"
)

type configuredBackend struct {
	checks     atomic.Int32
	closes     atomic.Int32
	closeError error
}

func (b *configuredBackend) Check(_ context.Context, request checkinputs.ScriptRequest) ([]checkinputs.ScriptDiagnostic, error) {
	b.checks.Add(1)
	before, _, _ := strings.Cut(request.Source, "missing")
	row := strings.Count(before, "\n") + 1
	column := len(before) - strings.LastIndex(before, "\n")
	code := "F821"
	if request.Filename == "actionlint.sh" {
		code = "SC2086"
	}
	return []checkinputs.ScriptDiagnostic{{Filename: request.Filename, Code: code, Message: "configured engine", Severity: "error", Location: checkinputs.ScriptPosition{Row: row, Column: column}, EndLocation: checkinputs.ScriptPosition{Row: row, Column: column + 7}}}, nil
}

func (b *configuredBackend) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.closes.Add(1)
	return b.closeError
}

func configuredEngineSelections() registry.Selections {
	selections := registry.Defaults()
	for name, selection := range selections {
		selection.Process = &processconfig.Options{Executable: new(""), AllowWasm: true}
		selections[name] = selection
	}
	return selections
}

func configuredEngineFixture(t *testing.T) (string, []string, *config.Loaded) {
	t.Helper()
	root := t.TempDir()
	source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(missing)\n        shell: python\npermissions: {}\n"
	cfgSource := "tools:\n  ruff:\n    backend:\n      wasm: {path: first.wasm}\noverrides:\n  - includes: ['**/night.yml']\n    tools:\n      ruff:\n        backend:\n          wasm: {path: second.wasm}\n"
	files := map[string]string{"actionlint.yaml": cfgSource}
	var paths []string
	for _, name := range []string{"ci.yml", "other.yml", "night.yml"} {
		path := filepath.Join(root, ".github", "workflows", name)
		files[path] = source
		paths = append(paths, path)
	}
	for path, content := range files {
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := exec.CommandContext(t.Context(), "git", "init", "--quiet", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	cfg, err := checking.ConfigurationRules().Read(filepath.Join(root, "actionlint.yaml"), os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	return root, paths, cfg
}

func runConfiguredAnalysis(t *testing.T, mode, root string, paths []string, cfg *config.Loaded, options checking.Options) (*checking.Result, error) {
	t.Helper()
	if options.Tools == nil {
		options.Tools = registry.Registrations(configuredEngineSelections())
	}
	switch mode {
	case "files":
		options.ConfigFile = "actionlint.yaml"
		return checking.Check(t.Context(), options, inputmodel.Request{Kind: inputmodel.Files, Paths: paths})
	case "repository":
		options.ConfigFile = "actionlint.yaml"
		return checking.Check(t.Context(), options, inputmodel.Request{Kind: inputmodel.Repository, Directory: root})
	default:
		var sources []workspace.Source
		for _, path := range paths {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			sources = append(sources, workspace.Source{Path: path, Content: content, Config: cfg.Config, ConfigProvenance: cfg.Provenance})
		}
		return checking.Analyze(t.Context(), options, sources)
	}
}

func TestConfigurationSelectedEnginesThroughPublicAPI(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"sources", "files", "repository"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			root, paths, cfg := configuredEngineFixture(t)
			loaded := map[string]*configuredBackend{}
			var loadedMu sync.Mutex
			options := checking.Options{WorkingDir: root, LoadScriptEngine: func(_ context.Context, path string) (checkinputs.ScriptBackend, error) {
				if path != filepath.Join(root, "first.wasm") && path != filepath.Join(root, "second.wasm") {
					t.Errorf("unresolved path: %s", path)
				}
				loadedMu.Lock()
				defer loadedMu.Unlock()
				if _, ok := loaded[path]; ok {
					t.Errorf("loaded twice: %s", path)
				}
				backend := &configuredBackend{}
				loaded[path] = backend
				return backend, nil
			}}
			result, err := runConfiguredAnalysis(t, mode, root, paths, cfg, options)
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || len(result.Diagnostics) != 3 || len(loaded) != 2 {
				t.Fatalf("result=%+v engines=%d", result, len(loaded))
			}
			for path, backend := range loaded {
				want := int32(2)
				if strings.HasSuffix(path, "second.wasm") {
					want = 1
				}
				if backend.checks.Load() != want || backend.closes.Load() != 1 {
					t.Errorf("%s checks=%d closes=%d", path, backend.checks.Load(), backend.closes.Load())
				}
			}
		})
	}
}

func TestConfiguredEngineErrorsAndExplicitDisable(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"sources", "files", "repository"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			root, paths, cfg := configuredEngineFixture(t)
			failure := errors.New("module failed")
			options := checking.Options{WorkingDir: root, LoadScriptEngine: func(context.Context, string) (checkinputs.ScriptBackend, error) { return nil, failure }}
			if _, err := runConfiguredAnalysis(t, mode, root, paths, cfg, options); !errors.Is(err, failure) {
				t.Fatalf("loader error lost: %v", err)
			}
			options.Tools = registry.Registrations(registry.Selections{"ruff": {Process: &processconfig.Options{Executable: new("")}}})
			if _, err := runConfiguredAnalysis(t, mode, root, paths, cfg, options); err != nil {
				t.Fatalf("explicit disable ignored: %v", err)
			}
			options.Tools = nil
			options.LoadScriptEngine = func(context.Context, string) (checkinputs.ScriptBackend, error) {
				return &configuredBackend{closeError: failure}, nil
			}
			result, err := runConfiguredAnalysis(t, mode, root, paths, cfg, options)
			if !errors.Is(err, failure) || result == nil || len(result.Diagnostics) != 3 {
				t.Fatalf("cleanup lost result/error: result=%+v err=%v", result, err)
			}
		})
	}
}

func TestConfiguredShellcheckAndCallerOwnedBackend(t *testing.T) {
	t.Parallel()
	root, paths, _ := configuredEngineFixture(t)
	cfgSource := "tools:\n  shellcheck:\n    backend:\n      wasm: {path: shellcheck.wasm}\n"
	if err := os.WriteFile(filepath.Join(root, "actionlint.yaml"), []byte(cfgSource), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo $missing\npermissions: {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	backend := &configuredBackend{}
	options := checking.Options{WorkingDir: root, ConfigFile: "actionlint.yaml", Tools: registry.Registrations(configuredEngineSelections()), LoadScriptEngine: func(context.Context, string) (checkinputs.ScriptBackend, error) { return backend, nil }}
	result, err := checking.Check(t.Context(), options, inputmodel.Request{Kind: inputmodel.Files, Paths: paths})
	if err != nil || result == nil || len(result.Diagnostics) != 3 || backend.checks.Load() != 3 || backend.closes.Load() != 1 {
		t.Fatalf("configured ShellCheck: result=%+v err=%v checks=%d closes=%d", result, err, backend.checks.Load(), backend.closes.Load())
	}
	explicit := &configuredBackend{}
	options.Tools = registry.Registrations(registry.Selections{"shellcheck": {Backend: explicit}})
	options.LoadScriptEngine = func(context.Context, string) (checkinputs.ScriptBackend, error) {
		t.Fatal("explicit backend must override configured engine")
		return nil, nil
	}
	if _, err := checking.Check(t.Context(), options, inputmodel.Request{Kind: inputmodel.Files, Paths: paths}); err != nil {
		t.Fatal(err)
	}
	if explicit.checks.Load() != 3 || explicit.closes.Load() != 0 {
		t.Fatalf("caller-owned backend checks=%d closes=%d", explicit.checks.Load(), explicit.closes.Load())
	}
}

func TestCompositeOnlyConfiguredEngine(t *testing.T) {
	t.Parallel()
	for _, tool := range []struct{ name, shell, script string }{
		{"ruff", "python", "print(missing)"},
		{"shellcheck", "bash", "echo $missing"},
	} {
		for _, mode := range []string{"sources", "files", "repository"} {
			t.Run(tool.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				root, paths, _ := configuredEngineFixture(t)
				cfgSource := fmt.Sprintf("overrides:\n  - includes: [inner/action.yml]\n    tools:\n      %s:\n        backend:\n          wasm: {path: composite.wasm}\n", tool.name)
				manifest := filepath.Join(root, "inner", "action.yml")
				if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
					t.Fatal(err)
				}
				files := map[string]string{
					filepath.Join(root, "actionlint.yaml"): cfgSource,
					paths[0]:                               fmt.Sprintf("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: %s\n        shell: %s\n      - uses: ./inner\npermissions: {}\n", tool.script, tool.shell),
					manifest:                               fmt.Sprintf("name: Inner\ndescription: Inner\nruns:\n  using: composite\n  steps:\n    - run: %s\n      shell: %s\n", tool.script, tool.shell),
				}
				for path, content := range files {
					if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				cfg, err := checking.ConfigurationRules().Read(filepath.Join(root, "actionlint.yaml"), os.ReadFile)
				if err != nil {
					t.Fatal(err)
				}
				backend := &configuredBackend{}
				var loads atomic.Int32
				options := checking.Options{WorkingDir: root, LoadScriptEngine: func(_ context.Context, path string) (checkinputs.ScriptBackend, error) {
					loads.Add(1)
					if path != filepath.Join(root, "composite.wasm") {
						t.Errorf("module path=%s", path)
					}
					return backend, nil
				}}
				wantChecks := int32(1)
				if mode == "repository" {
					// Discovery also checks the action as a standalone document.
					wantChecks = 2
				}
				result, err := runConfiguredAnalysis(t, mode, root, paths[:1], cfg, options)
				if err != nil || result == nil || len(result.Diagnostics) != 1 || backend.checks.Load() != wantChecks || loads.Load() != 1 || backend.closes.Load() != 1 {
					t.Fatalf("composite-only: result=%+v err=%v checks=%d loads=%d closes=%d", result, err, backend.checks.Load(), loads.Load(), backend.closes.Load())
				}
				if result.Diagnostics[0].Rule != tool.name || filepath.ToSlash(result.Diagnostics[0].Path) != "inner/action.yml" {
					t.Fatalf("unexpected diagnostic: %+v", result.Diagnostics[0])
				}
				disabled := scriptbackend.Selection{Process: &processconfig.Options{Executable: new("")}}
				options.Tools = registry.Registrations(registry.Selections{tool.name: disabled})
				result, err = runConfiguredAnalysis(t, mode, root, paths[:1], cfg, options)
				if err != nil || result == nil || len(result.Diagnostics) != 0 || loads.Load() != 1 || backend.checks.Load() != wantChecks || backend.closes.Load() != 1 {
					t.Fatalf("explicit disable: result=%+v err=%v checks=%d loads=%d closes=%d", result, err, backend.checks.Load(), loads.Load(), backend.closes.Load())
				}
			})
		}
	}
}

func TestInlineConfiguredEnginePaths(t *testing.T) {
	for _, constructed := range []bool{false, true} {
		t.Run(fmt.Sprintf("constructed=%t", constructed), func(t *testing.T) {
			root, paths, _ := configuredEngineFixture(t)
			t.Chdir(t.TempDir())
			cfg, err := checking.ConfigurationRules().Parse([]byte("tools: {ruff: {backend: {wasm: {path: engines/../first.wasm}}}}"))
			if err != nil {
				t.Fatal(err)
			}
			if constructed {
				encoded, err := checking.ConfigurationRules().Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				cfg, err = checking.ConfigurationRules().Parse(encoded)
				if err != nil {
					t.Fatal(err)
				}
			}
			other, err := checking.ConfigurationRules().Parse([]byte("tools: {ruff: {backend: {wasm: {path: first.wasm}}}}"))
			if err != nil {
				t.Fatal(err)
			}
			backend := &configuredBackend{}
			var loads atomic.Int32
			options := checking.Options{WorkingDir: root, Tools: registry.Registrations(configuredEngineSelections()), LoadScriptEngine: func(_ context.Context, path string) (checkinputs.ScriptBackend, error) {
				loads.Add(1)
				if path != filepath.Join(root, "first.wasm") {
					t.Errorf("unresolved inline module: %s", path)
				}
				return backend, nil
			}}
			var sources []workspace.Source
			for i, selected := range []*config.Config{cfg, other} {
				content, err := os.ReadFile(paths[i])
				if err != nil {
					t.Fatal(err)
				}
				sources = append(sources, workspace.Source{Path: paths[i], Content: content, Config: selected})
			}
			result, err := checking.Analyze(t.Context(), options, sources)
			if err != nil || result == nil || len(result.Diagnostics) != 2 || loads.Load() != 1 || backend.checks.Load() != 2 || backend.closes.Load() != 1 {
				t.Fatalf("inline engine: result=%+v err=%v loads=%d checks=%d closes=%d", result, err, loads.Load(), backend.checks.Load(), backend.closes.Load())
			}
			moduleInputs := 0
			for _, path := range result.Inputs {
				if strings.HasSuffix(path, "first.wasm") {
					moduleInputs++
					if path != filepath.Join(root, "first.wasm") {
						t.Errorf("incorrect module input: %s", path)
					}
				}
			}
			if moduleInputs != 1 {
				t.Errorf("module inputs=%d, want 1", moduleInputs)
			}
			original, err := checking.ConfigurationRules().Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			otherOriginal, err := checking.ConfigurationRules().Marshal(other)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(original), "engines/../first.wasm") || !strings.Contains(string(otherOriginal), "path: first.wasm") {
				t.Fatal("analysis mutated caller configuration")
			}
		})
	}
}

func TestConfiguredEngineURIs(t *testing.T) {
	for _, scheme := range []string{"http", "https"} {
		for _, mode := range []string{"sources", "files", "repository"} {
			t.Run(scheme+"/"+mode, func(t *testing.T) {
				root, paths, _ := configuredEngineFixture(t)
				t.Chdir(t.TempDir())
				uri := scheme + "://example.com/engine.wasm?version=1#sha256=" + strings.Repeat("a", 64)
				configPath := filepath.Join(root, "actionlint.yaml")
				if err := os.WriteFile(configPath, []byte("tools: {ruff: {backend: {wasm: {path: '"+uri+"'}}}}"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfg, err := checking.ConfigurationRules().Read(configPath, os.ReadFile)
				if err != nil {
					t.Fatal(err)
				}
				backend := &configuredBackend{}
				var loads atomic.Int32
				options := checking.Options{WorkingDir: root, LoadScriptEngine: func(_ context.Context, source string) (checkinputs.ScriptBackend, error) {
					loads.Add(1)
					if source != uri {
						t.Errorf("changed engine URI: got %q, want %q", source, uri)
					}
					return backend, nil
				}}
				result, err := runConfiguredAnalysis(t, mode, root, paths[:2], cfg, options)
				if err != nil || result == nil || loads.Load() != 1 || backend.closes.Load() != 1 {
					t.Fatalf("URI engine: result=%+v err=%v loads=%d closes=%d", result, err, loads.Load(), backend.closes.Load())
				}
				foundConfig := false
				for _, path := range result.Inputs {
					if strings.Contains(path, "example.com") {
						t.Errorf("remote engine treated as local input: %q", path)
					}
					foundConfig = foundConfig || path == configPath
				}
				if !foundConfig {
					t.Error("engine configuration missing from local inputs")
				}
			})
		}
	}
}
