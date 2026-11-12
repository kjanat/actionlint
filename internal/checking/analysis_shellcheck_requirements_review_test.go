package checking_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestShellcheckRequiredToolsCompositeBackends(t *testing.T) {
	t.Parallel()
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
		// The composite action's own configuration decides whether its
		// scripts are checked, whatever the calling workflow's says.
		{"caller rule disabled", `tools: {shellcheck: {backend: {wasm: {path: engine.wasm}}}}
overrides: [{includes: ['.github/workflows/**'], lint: {rules: {external: {shellcheck: off}}}}, {includes: ['shell-action/**'], tools: {shellcheck: {backend: {process: {executable: shellcheck}}}}}]`, true},
		{"caller tool disabled", `tools: {shellcheck: {backend: {wasm: {path: engine.wasm}}}}
overrides: [{includes: ['.github/workflows/**'], tools: {shellcheck: false}}, {includes: ['shell-action/**'], tools: {shellcheck: {enabled: true, backend: {process: {executable: shellcheck}}}}}]`, true},
		{"workflow-only wasm preserves process baseline", `tools: {shellcheck: {backend: {process: {executable: shellcheck}}}}
overrides: [{includes: ['.github/workflows/**'], tools: {shellcheck: {backend: {wasm: {path: workflow.wasm}}}}}]`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFixture(t, root, ".git", "")
			workflow := writeFixture(t, root, ".github/workflows/ci.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps: [{uses: $/shell-action}]\n")
			writeFixture(t, root, "shell-action/action.yml", "name: Shell\ndescription: test\nruns:\n  using: composite\n  steps: [{shell: bash, run: echo hello}]\n")
			writeFixture(t, root, ".github/actionlint.yaml", tc.config)
			for _, command := range []string{"shellcheck", ""} {
				options := checking.Options{WorkingDir: root, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: command}})}
				needed, err := checking.RequiredTools(t.Context(), options, []string{workflow})
				if err != nil || needed["shellcheck"] != (tc.want && command != "") {
					t.Fatalf("command %q: needed=%+v, error=%v", command, needed, err)
				}
			}
		})
	}
}

func TestShellcheckRequiredToolsExplicitProcessAndCancellation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	config := writeFixture(t, root, "actionlint.yml", "tools: {shellcheck: {backend: {wasm: {path: never-load.wasm}}}}")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	options := checking.Options{
		WorkingDir: root, ConfigFile: config,
		Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: "shellcheck", Process: &processconfig.Options{}}}),
	}
	needed, err := checking.RequiredTools(ctx, options, []string{"never-read.yml"})
	if err != nil || !needed["shellcheck"] {
		t.Fatalf("explicit host process must override Wasm: %+v, %v", needed, err)
	}
	cancel()
	if _, err := checking.RequiredTools(ctx, options, []string{"never-read.yml"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled plan: %v", err)
	}
}

func TestShellcheckProcessPlanMatchesCompositeExecution(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeFixture(t, root, ".git", "")
	workflow := writeFixture(t, root, ".github/workflows/ci.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo missing\n      - uses: $/shell-action\n")
	writeFixture(t, root, "shell-action/action.yml", "name: Shell\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo hello\n")
	writeFixture(t, root, ".github/actionlint.yaml", `tools: {shellcheck: {backend: {wasm: {path: engine.wasm}}}}
overrides: [{includes: ['shell-action/**'], tools: {shellcheck: {backend: {process: {executable: repository-command-must-not-run}}}}}]
`)
	backend := &recordingEngine{code: "SC2086"}
	options := checking.Options{
		WorkingDir: root,
		Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: "shellcheck", Process: &processconfig.Options{
			Executable: &executable, AllowWasm: true,
			LauncherArguments: []string{"-test.run=^TestShellcheckPlanningHelper$", "--", "shellcheck"},
		}}}),
		LoadScriptEngine: func(context.Context, string) (checkinputs.ScriptBackend, error) { return backend, nil },
	}
	needed, err := checking.RequiredTools(t.Context(), options, []string{workflow})
	if err != nil || !needed["shellcheck"] {
		t.Fatalf("mixed workflow/composite requires the host process: %+v, %v", needed, err)
	}
	result, err := checking.Check(t.Context(), options, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{workflow}})
	if err != nil {
		t.Fatal(err)
	}
	if len(backend.requests) != 1 || !slices.ContainsFunc(result.Diagnostics, func(d results.Diagnostic) bool {
		return d.Rule == "shellcheck" && d.Code == "SC9999" && filepath.Clean(d.Path) == filepath.Join("shell-action", "action.yml")
	}) {
		t.Fatalf("workflow must use Wasm and composite the provisioned process: requests=%+v diagnostics=%+v", backend.requests, result.Diagnostics)
	}
}
