package githubaction

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	actionlint "actionlint.kjanat.dev/internal/engine"
)

func TestProvisionedRuffPreservesPerFileWasmSelection(t *testing.T) {
	command, err := exec.LookPath("ruff")
	if err != nil {
		t.Fatal(err)
	}
	module, err := filepath.Abs(filepath.Join("..", "corewasm", "testdata", "engines", "echo.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf("tools:\n  shellcheck: false\n  ruff:\n    backend:\n      process: {executable: repository-must-not-select-this}\noverrides:\n  - includes: ['**/wasm.yml']\n    tools:\n      ruff:\n        backend:\n          wasm: {path: %q}\n", filepath.ToSlash(module))
	root := workspaceWith(t, map[string]string{
		".git": "", ".github/actionlint.yaml": config,
		".github/workflows/native.yml": "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(missing)\n        shell: python\n",
		".github/workflows/wasm.yml":   "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: missing = 1\n        shell: python\n",
	})
	env := map[string]string{"GITHUB_WORKSPACE": root, "INPUT_RUFF": "true", "ACTIONLINT_RUFF_COMMAND": command}
	lookup := func(key string) string { return env[key] }
	needed, err := requiredTools(lookup)
	if err != nil || !needed.Ruff {
		t.Fatalf("needed=%+v err=%v", needed, err)
	}
	req := &lintRequest{ctx: t.Context(), workingDir: root, workspaceDir: root, format: formatJSON}
	if err := req.configureEnvironment(lookup); err != nil {
		t.Fatal(err)
	}
	result := runLinter(req)
	if result.code != actionlint.ExitStatusSuccessProblemFound || len(result.diagnostics) != 2 {
		t.Fatalf("result=%+v diagnostics=%+v", result.lintOutcome, result.diagnostics)
	}
	for _, finding := range result.diagnostics {
		if finding.Code != "F821" {
			t.Fatalf("finding=%+v", finding)
		}
	}
}

func TestActionRunsConfiguredWasmWithoutNativeProvisioning(t *testing.T) {
	module, err := os.ReadFile(filepath.Join("..", "corewasm", "testdata", "engines", "echo.wasm"))
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
			artifact := module
			if tc.name == "shellcheck" {
				// Keep the fixture's Wasm data lengths unchanged while supplying
				// the SC-prefixed diagnostic required by the ShellCheck bridge.
				if bytes.Count(module, []byte("F821")) != 1 {
					t.Fatal("echo fixture must contain exactly one diagnostic code")
				}
				artifact = bytes.Replace(module, []byte("F821"), []byte(tc.code), 1)
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
				"GITHUB_WORKSPACE": root, "INPUT_RUFF": "true", "INPUT_FORMAT": "json",
				"INPUT_FILES": ".github/workflows/ci.yml",
			}
			lookup := func(key string) string { return env[key] }
			needed, err := requiredTools(lookup)
			if err != nil || needed.Ruff || needed.Shellcheck {
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
			if result.code != actionlint.ExitStatusSuccessProblemFound || len(result.diagnostics) != 1 || result.diagnostics[0].Code != tc.code {
				t.Fatalf("result=%+v diagnostics=%+v", result.lintOutcome, result.diagnostics)
			}
		})
	}
}

func TestActionToolOptionsPreserveCallerSelection(t *testing.T) {
	executable := "explicit-native-command"
	for _, tc := range []struct {
		name    string
		options *actionlint.ExternalCommandOptions
	}{
		{"defaults", nil},
		{"native", &actionlint.ExternalCommandOptions{Executable: &executable}},
		{"ignore-config", &actionlint.ExternalCommandOptions{IgnoreConfig: true}},
		{"optional", &actionlint.ExternalCommandOptions{Optional: true}},
		{"wasm", &actionlint.ExternalCommandOptions{AllowWasm: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := actionlint.ExternalCommandOptions{AllowWasm: true}
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
			req := &lintRequest{shellcheck: "shellcheck", shellcheckOptions: tc.options}
			if err := req.configureShellcheck(func(string) string { return "" }); err != nil {
				t.Fatal(err)
			}
			if req.shellcheckOptions.AllowWasm != before.AllowWasm || req.shellcheckOptions.Optional != before.Optional || req.shellcheckOptions.IgnoreConfig != before.IgnoreConfig || req.shellcheckOptions.Executable != before.Executable {
				t.Fatalf("ShellCheck changed backend selection: %+v, want %+v", req.shellcheckOptions, before)
			}
		})
	}
}
