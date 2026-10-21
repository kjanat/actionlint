package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"actionlint.kjanat.dev/analyzer"
)

func TestShellcheckRequiredToolsCompositeBackends(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		want         bool
	}{
		{"all wasm", "tools: {shellcheck: {backend: {wasm: {path: engine.wasm}}}}", false},
		{"composite process", `tools: {shellcheck: {backend: {wasm: {path: engine.wasm}}}}
overrides: [{includes: ['shell-action/**'], tools: {shellcheck: {backend: {process: {executable: shellcheck}}}}}]`, true},
		{"process then catch-all wasm", `tools: {shellcheck: {backend: {wasm: {path: engine.wasm}}}}
overrides: [{includes: ['shell-action/**'], tools: {shellcheck: {backend: {process: {executable: shellcheck}}}}}, {includes: ['**'], tools: {shellcheck: {backend: {wasm: {path: final.wasm}}}}}]`, false},
		{"catch-all wasm then process", `tools: {shellcheck: {backend: {wasm: {path: engine.wasm}}}}
overrides: [{includes: ['**'], tools: {shellcheck: {backend: {wasm: {path: final.wasm}}}}}, {includes: ['shell-action/**'], tools: {shellcheck: {backend: {process: {executable: shellcheck}}}}}]`, true},
		{"disabled process", `tools: {shellcheck: {backend: {wasm: {path: engine.wasm}}}}
overrides: [{includes: ['shell-action/**'], tools: {shellcheck: {enabled: false, backend: {process: {executable: shellcheck}}}}}]`, false},
		{"disabled process rule", `tools: {shellcheck: {backend: {wasm: {path: engine.wasm}}}}
overrides: [{includes: ['shell-action/**'], tools: {shellcheck: {backend: {process: {executable: shellcheck}}}}, lint: {rules: {external: {shellcheck: off}}}}]`, false},
		{"caller lint disabled", `tools: {shellcheck: {backend: {wasm: {path: engine.wasm}}}}
overrides: [{includes: ['.github/workflows/**'], lint: {enabled: false}}, {includes: ['shell-action/**'], tools: {shellcheck: {backend: {process: {executable: shellcheck}}}}}]`, false},
		{"caller rule disabled", `tools: {shellcheck: {backend: {wasm: {path: engine.wasm}}}}
overrides: [{includes: ['.github/workflows/**'], lint: {rules: {external: {shellcheck: off}}}}, {includes: ['shell-action/**'], tools: {shellcheck: {backend: {process: {executable: shellcheck}}}}}]`, false},
		{"caller tool disabled", `tools: {shellcheck: {backend: {wasm: {path: engine.wasm}}}}
overrides: [{includes: ['.github/workflows/**'], tools: {shellcheck: false}}, {includes: ['shell-action/**'], tools: {shellcheck: {enabled: true, backend: {process: {executable: shellcheck}}}}}]`, false},
		{"workflow-only wasm preserves process baseline", `tools: {shellcheck: {backend: {process: {executable: shellcheck}}}}
overrides: [{includes: ['.github/workflows/**'], tools: {shellcheck: {backend: {wasm: {path: workflow.wasm}}}}}]`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeShellcheckFixture(t, root, ".git", "")
			workflow := writeShellcheckFixture(t, root, ".github/workflows/ci.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{uses: $/shell-action}]\n")
			writeShellcheckFixture(t, root, "shell-action/action.yml", "name: Shell\ndescription: test\nruns:\n  using: composite\n  steps: [{shell: bash, run: echo hello}]\n")
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", tc.config)
			for _, command := range []string{"shellcheck", ""} {
				session, err := NewAnalysisSession(AnalysisOptions{Context: t.Context(), WorkingDir: root, Shellcheck: command})
				if err != nil {
					t.Fatal(err)
				}
				needed, err := session.RequiredTools([]string{workflow})
				if err != nil || needed.Shellcheck != (tc.want && command != "") {
					t.Fatalf("command %q: needed=%+v, error=%v", command, needed, err)
				}
				if err := session.Close(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestShellcheckRequiredToolsExplicitProcessAndCancellation(t *testing.T) {
	root := t.TempDir()
	config := writeShellcheckFixture(t, root, "actionlint.yml", "tools: {shellcheck: {backend: {wasm: {path: never-load.wasm}}}}")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	session, err := NewAnalysisSession(AnalysisOptions{
		Context: ctx, WorkingDir: root, ConfigFile: config, Shellcheck: "shellcheck",
		ShellcheckOptions: &ExternalCommandOptions{},
	})
	if err != nil {
		t.Fatal(err)
	}
	needed, err := session.RequiredTools([]string{"never-read.yml"})
	if err != nil || !needed.Shellcheck {
		t.Fatalf("explicit host process must override Wasm: %+v, %v", needed, err)
	}
	cancel()
	if _, err := session.RequiredTools([]string{"never-read.yml"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled plan: %v", err)
	}
	if err := session.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestShellcheckProcessPlanMatchesCompositeExecution(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeShellcheckFixture(t, root, ".git", "")
	workflow := writeShellcheckFixture(t, root, ".github/workflows/ci.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo missing\n      - uses: $/shell-action\n")
	writeShellcheckFixture(t, root, "shell-action/action.yml", "name: Shell\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo hello\n")
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", `tools: {shellcheck: {backend: {wasm: {path: engine.wasm}}}}
overrides: [{includes: ['shell-action/**'], tools: {shellcheck: {backend: {process: {executable: repository-command-must-not-run}}}}}]
`)
	backend := &recordingEngine{code: "SC2086"}
	session, err := NewAnalysisSession(AnalysisOptions{
		Context: t.Context(), WorkingDir: root, Shellcheck: "shellcheck",
		ShellcheckOptions: &ExternalCommandOptions{
			Executable: &executable, AllowWasm: true,
			LauncherArguments: []string{"-test.run=^TestKnownShellCommandHelper$", "--", "shellcheck"},
		},
		LoadScriptEngine: func(context.Context, string) (analyzer.ScriptBackend, error) { return backend, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := session.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	}()
	needed, err := session.RequiredTools([]string{workflow})
	if err != nil || !needed.Shellcheck {
		t.Fatalf("mixed workflow/composite requires the host process: %+v, %v", needed, err)
	}
	result, err := session.Files([]string{workflow}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(backend.requests) != 1 || !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool {
		return d.Rule == "shellcheck" && d.Code == "SC9999" && filepath.Clean(d.Path) == filepath.Join("shell-action", "action.yml")
	}) {
		t.Fatalf("workflow must use Wasm and composite the provisioned process: requests=%+v diagnostics=%+v", backend.requests, result.Diagnostics)
	}
}
