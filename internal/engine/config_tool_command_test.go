package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/repoconfig"

	"actionlint.kjanat.dev/internal/toolconfig"
)

func TestProjectToolBackendPrecedence(t *testing.T) {
	cfg := &Config{}
	cfg.Tools.Ruff.Backend = &toolconfig.Backend{Process: &toolconfig.Command{
		Executable: "mise", Args: []string{"exec", "ruff", "--", "ruff"}, Env: map[string]string{"CUSTOM": "literal $value"},
	}}
	automatic := &ExternalCommandOptions{Optional: true, Arguments: []string{"--select", "F"}}
	selected := projectToolOptions("ruff", "ruff", automatic, cfg, "/analysis")
	if selected.Executable == nil || *selected.Executable != "mise" || !reflect.DeepEqual(selected.LauncherArguments, cfg.Tools.Ruff.Backend.Process.Args) || !reflect.DeepEqual(selected.Arguments, automatic.Arguments) || !reflect.DeepEqual(selected.Environment, []string{"CUSTOM=literal $value"}) {
		t.Fatalf("wrong projection: %+v", selected)
	}
	explicit := &ExternalCommandOptions{IgnoreConfig: true}
	if got := projectToolOptions("ruff", "ruff", explicit, cfg, "/analysis"); got != explicit {
		t.Fatal("explicit selection overridden")
	}
	if got := projectToolOptions("ruff", "", automatic, cfg, "/analysis"); got != automatic {
		t.Fatal("explicit disable overridden")
	}
}

func TestProvisionedProcessAllowsOnlyConfiguredWasm(t *testing.T) {
	for _, name := range []string{"ruff", "shellcheck"} {
		t.Run(name, func(t *testing.T) {
			cfg := &Config{}
			backend := &toolconfig.Backend{Wasm: &toolconfig.Module{Path: "selected.wasm"}}
			if name == "ruff" {
				cfg.Tools.Ruff.Backend = backend
			} else {
				cfg.Tools.Shellcheck.Backend = backend
			}
			for _, tc := range []struct {
				name    string
				options *ExternalCommandOptions
				want    bool
			}{
				{"automatic", &ExternalCommandOptions{Optional: true}, true},
				{"explicit process", &ExternalCommandOptions{}, false},
				{"provisioned process", &ExternalCommandOptions{AllowWasm: true}, true},
				{"explicit override", &ExternalCommandOptions{AllowWasm: true, IgnoreConfig: true}, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if got := configuredScriptModule(name, name, tc.options, cfg); (got != nil) != tc.want {
						t.Fatalf("selected=%+v want=%t", got, tc.want)
					}
				})
			}
			backend.Wasm = nil
			backend.Process = &toolconfig.Command{Executable: "untrusted"}
			if got := projectToolCommand(name, name, &ExternalCommandOptions{AllowWasm: true}, cfg); got != nil {
				t.Fatalf("repository process replaced provisioned command: %+v", got)
			}
		})
	}
}

func TestInheritedBackendExecutableOrigins(t *testing.T) {
	root := t.TempDir()
	parentDir := filepath.Join(root, "parent")
	if err := os.Mkdir(parentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(parentDir, "base.yaml")
	if err := os.WriteFile(parent, []byte(`tools:
  ruff:
    backend:
      process: {executable: ./bin/ruff}
overrides:
  - includes: ['*.yml']
    tools:
      shellcheck:
        backend:
          process: {executable: ./bin/shellcheck}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "actionlint.yaml")
	if err := os.WriteFile(child, []byte("extends: [./parent/base.yaml]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := ReadConfigFile(child)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Tools.Ruff.Backend.Process.Executable; got != filepath.Join(parentDir, "bin", "ruff") {
		t.Fatal(got)
	}
	effective, err := repoconfig.ForFile(cfg, filepath.Join(root, "ci.yml"), root)
	if err != nil {
		t.Fatal(err)
	}
	if got := effective.Tools.Shellcheck.Backend.Process.Executable; got != filepath.Join(parentDir, "bin", "shellcheck") {
		t.Fatal(got)
	}
}

func TestConfiguredBackendProcessHelper(t *testing.T) {
	if os.Getenv("ACTIONLINT_BACKEND_HELPER") != "1" {
		return
	}
	index := slices.Index(os.Args, "--")
	if index < 0 || len(os.Args) < index+3 || os.Args[index+1] != "literal $value; with spaces" || os.Getenv("ACTIONLINT_BACKEND_LITERAL") != "$(not a command)" {
		os.Exit(2)
	}
	args := os.Args[index+2:]
	source, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(2)
	}
	row := 1
	for i, line := range strings.Split(string(source), "\n") {
		if line == "echo" {
			row = i + 1
			break
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		os.Exit(2)
	}
	if args[0] == "check" {
		encoded, _ := json.Marshal([]map[string]any{{"filename": filepath.Join(cwd, "actionlint.py"), "code": "F821", "message": "backend process finding", "location": map[string]int{"row": 1, "column": 1}, "end_location": map[string]int{"row": 1, "column": 2}}})
		_, _ = os.Stdout.Write(encoded)
	} else {
		_, _ = fmt.Fprintf(os.Stdout, `{"comments":[{"file":"-","line":%d,"endLine":%d,"column":1,"endColumn":2,"level":"warning","code":2086,"message":"backend process finding"}]}`, row, row)
	}
	os.Exit(1)
}

func TestConfiguredBackendLiteralArgumentsAndEnvironment(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	process := &toolconfig.Backend{Process: &toolconfig.Command{
		Executable: executable,
		Args:       []string{"-test.run=^TestConfiguredBackendProcessHelper$", "--", "literal $value; with spaces"},
		Env:        map[string]string{"ACTIONLINT_BACKEND_HELPER": "1", "ACTIONLINT_TEST_STDIN_HELPER": "1", "ACTIONLINT_BACKEND_LITERAL": "$(not a command)"},
	}}
	for _, name := range []string{"ruff", "shellcheck"} {
		t.Run(name, func(t *testing.T) {
			cfg := &Config{}
			request := AnalysisRequest{WorkingDir: t.TempDir(), AllowConfigProcesses: true}
			shell := "python"
			if name == "ruff" {
				cfg.Tools.Ruff.Backend = process
				request.Ruff = "ruff"
			} else {
				cfg.Tools.Shellcheck.Backend = process
				request.ShellCheck = "shellcheck"
				shell = "bash"
			}
			request.Sources = []SourceUnit{{Path: "ci.yml", Config: cfg, Content: []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: " + shell + "\n        run: echo\n")}}
			result, err := Analyze(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != name {
				t.Fatalf("invocation lost: %+v", result.Diagnostics)
			}
		})
	}
}
