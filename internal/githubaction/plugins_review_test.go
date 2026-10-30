package githubaction

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/localapp"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/render"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
)

func TestRetiredProvisioningPreservesConfiguredWasmSelection(t *testing.T) {
	module, err := os.ReadFile(filepath.Join("..", "plugins", "core", "testdata", "engines", "echo.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	module, err = testutil.ReplaceWasmDataString(module, []byte(`"engine":{"name":"echo"`), []byte(`"engine":{"name":"ruff"`))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ruff.wasm")
	if err := os.WriteFile(path, module, 0o600); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf("tools:\n  shellcheck: false\n  ruff:\n    backend:\n      wasm: {path: %q}\n", filepath.ToSlash(path))
	root := workspaceWith(t, map[string]string{
		".git": "", ".github/actionlint.yaml": config,
		".github/workflows/wasm.yml": "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: missing = 1\n        shell: python\n",
	})
	env := map[string]string{"GITHUB_WORKSPACE": root, "INPUT_RUFF": "true", "ACTIONLINT_RUFF_COMMAND": "retired-command-must-not-run"}
	lookup := func(key string) string { return env[key] }
	req := &lintRequest{ctx: t.Context(), workingDir: root, workspaceDir: root, format: formatJSON}
	if err := req.configureEnvironment(lookup); err != nil {
		t.Fatal(err)
	}
	result := runLinter(req)
	if result.code != render.ExitSuccessProblemFound || len(result.diagnostics) != 1 {
		t.Fatalf("result=%+v diagnostics=%+v", result.lintOutcome, result.diagnostics)
	}
	for _, finding := range result.diagnostics {
		if finding.Code != "F821" {
			t.Fatalf("finding=%+v", finding)
		}
	}
}

func TestActionRunsConfiguredWasmWithoutNativeProvisioning(t *testing.T) {
	module, err := os.ReadFile(filepath.Join("..", "plugins", "core", "testdata", "engines", "echo.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, other, shell, source, code string }{
		{"ruff", "shellcheck", "python", "missing", "F821"},
		// A shebang stays before generated ShellCheck directives, keeping the
		// single-line echo fixture's diagnostic on an original source line.
		{"shellcheck", "ruff", "bash", "#!/bin/bash missing", "SC21"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldEngine := []byte(`"engine":{"name":"echo"`)
			newEngine := []byte(`"engine":{"name":"ruff"`)
			if tc.name == "shellcheck" {
				oldEngine = []byte(`"name":"Echo","plugin_version":"0.1.0","engine":{"name":"echo","version":"1.0.0"},`)
				newEngine = []byte(`"name":"E","plugin_version":"0","engine":{"name":"shellcheck","version":"1.0.0"} ,`)
			}
			artifact, err := testutil.ReplaceWasmDataString(module, oldEngine, newEngine)
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "shellcheck" {
				// Supply the SC-prefixed diagnostic required by the ShellCheck bridge.
				if bytes.Count(artifact, []byte("F821")) != 1 {
					t.Fatal("echo fixture must contain exactly one diagnostic code")
				}
				artifact = bytes.Replace(artifact, []byte("F821"), []byte(tc.code), 1)
				oldShells := []byte(`"script_shells":["python","python3"]`)
				newShells := []byte(`"script_shells":["bash","sh"]`)
				if bytes.Count(artifact, oldShells) != 1 {
					t.Fatal("echo fixture must contain exactly one shell catalogue")
				}
				newShells = append(newShells, bytes.Repeat([]byte(" "), len(oldShells)-len(newShells))...)
				artifact = bytes.Replace(artifact, oldShells, newShells, 1)
			}
			path := filepath.Join(t.TempDir(), "engine.wasm")
			if err := os.WriteFile(path, artifact, 0o600); err != nil {
				t.Fatal(err)
			}
			config := fmt.Sprintf("tools:\n  %s: false\n  %s:\n    backend:\n      wasm: {path: %q}\n", tc.other, tc.name, filepath.ToSlash(path))
			workflow := fmt.Sprintf("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: %q\n        shell: %s\n", tc.source, tc.shell)
			root := workspaceWith(t, map[string]string{
				".git": "", ".github/actionlint.yaml": config,
				".github/workflows/ci.yml": workflow,
			})
			// Neither backend may fall back to an installed native executable.
			t.Setenv("PATH", t.TempDir())
			env := map[string]string{
				"GITHUB_WORKSPACE": root, "INPUT_FORMAT": "json",
				"INPUT_FILES": ".github/workflows/ci.yml", "INPUT_RUFF": "true",
			}
			lookup := func(key string) string { return env[key] }
			plan, err := provisioningRequest(lookup)
			if err != nil {
				t.Fatal(err)
			}
			selected, err := plan.pluginSelection().Resolve(t.Context(), plan.workingDir, plan.pluginInput())
			if err != nil {
				t.Fatal(err)
			}
			needed, err := localapp.RequiredToolsSources(t.Context(), application.Options{
				WorkingDir: plan.workingDir, ConfigFile: plan.configFile, ConfigOverlays: plan.overlays,
				IgnorePatterns: plan.ignore, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: plan.shellcheck}, "ruff": {Command: plan.ruff}}),
			}, plan.files, selected)
			if err != nil || needed["ruff"] || needed["shellcheck"] {
				t.Fatalf("needed=%+v err=%v", needed, err)
			}
			in, err := parseInputs(environmentArgs(lookup))
			if err != nil {
				t.Fatal(err)
			}
			req, err := (&action{env: lookup}).prepareRequest(in, root)
			if err != nil {
				t.Fatal(err)
			}
			req.ctx = t.Context()
			result := runLinter(req)
			if result.code != render.ExitSuccessProblemFound || len(result.diagnostics) != 1 || result.diagnostics[0].Code != tc.code {
				t.Fatalf("result=%+v diagnostics=%+v", result.lintOutcome, result.diagnostics)
			}
		})
	}
}

func TestActionToolOptionsPreserveCallerSelection(t *testing.T) {
	executable := "explicit-native-command"
	for _, tc := range []struct {
		name    string
		options *processconfig.Options
	}{
		{"defaults", nil},
		{"native", &processconfig.Options{Executable: &executable}},
		{"ignore-config", &processconfig.Options{IgnoreConfig: true}},
		{"optional", &processconfig.Options{Optional: true}},
		{"wasm", &processconfig.Options{AllowWasm: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := processconfig.Options{Optional: true}
			if tc.options != nil {
				want = *tc.options
			}
			before := want
			want.WorkingDir = "action-directory"
			got := toolOptionsInDirectory(tc.options, want.WorkingDir)
			if !reflect.DeepEqual(*got, want) {
				t.Fatalf("options=%+v, want %+v", got, want)
			}
			if tc.options != nil && !reflect.DeepEqual(*tc.options, before) {
				t.Fatalf("caller options changed: %+v", tc.options)
			}
		})
	}
}

func TestActionRunsConfiguredScriptComponent(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "plugins", "component", "testdata", "plugins", "ruff-script-engine.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	module := filepath.Join(t.TempDir(), "ruff-component.wasm")
	if err := os.WriteFile(module, data, 0o600); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf("tools:\n  shellcheck: false\n  ruff:\n    backend:\n      wasm: {path: %q}\n", filepath.ToSlash(module))
	root := workspaceWith(t, map[string]string{
		".git": "", ".github/actionlint.yaml": config,
		".github/workflows/ci.yml": "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(missing)\n        shell: python\n",
	})
	req := &lintRequest{ctx: t.Context(), workingDir: root, workspaceDir: root, format: formatJSON, ruff: "ruff"}
	result := runLinter(req)
	if result.code != render.ExitSuccessProblemFound || len(result.diagnostics) != 1 || result.diagnostics[0].Code != "ECHO001" {
		t.Fatalf("result=%+v diagnostics=%+v", result.lintOutcome, result.diagnostics)
	}
	if result.diagnostics[0].Start.Line != 6 || result.diagnostics[0].Start.Column != 20 {
		t.Fatalf("component finding lost source location: %+v", result.diagnostics[0])
	}
}
