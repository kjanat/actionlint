package tools_test

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	toolconfig "actionlint.kjanat.dev/v2/internal/input/configuration/tools"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestConfiguredBackendProcessHelper(t *testing.T) {
	t.Parallel()
	if os.Getenv("ACTIONLINT_BACKEND_HELPER") != "1" {
		return
	}
	index := slices.Index(os.Args, "--")
	if index < 0 || len(os.Args) < index+3 || os.Args[index+1] != "literal $value; with spaces" || os.Getenv("ACTIONLINT_BACKEND_LITERAL") != "$(not a command)" {
		os.Exit(2)
	}
	args := os.Args[index+2:]
	if args[0] == "--version" {
		// The bundled Ruff provider checks the minimum version first.
		fmt.Print("ruff 0.17.0\n")
		os.Exit(0)
	}
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
	t.Parallel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command, err := toolconfig.NewCommand(executable,
		[]string{"-test.run=^TestConfiguredBackendProcessHelper$", "--", "literal $value; with spaces"},
		map[string]string{"ACTIONLINT_BACKEND_HELPER": "1", "ACTIONLINT_TEST_STDIN_HELPER": "1", "ACTIONLINT_BACKEND_LITERAL": "$(not a command)"})
	if err != nil {
		t.Fatal(err)
	}
	process := toolconfig.ProcessBackend(command)
	for _, name := range []string{"ruff", "shellcheck"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := &repoconfig.Config{}
			request := checking.Options{WorkingDir: t.TempDir(), AllowConfigProcesses: true}
			var requestSources []workspace.Source
			shell := "python"
			if name == "ruff" {
				cfg.Tools = repoconfig.ToolsConfig{"ruff": {Backend: process}}
				request.Tools = registry.Registrations(registry.Selections{"ruff": {Command: "ruff"}})
			} else {
				cfg.Tools = repoconfig.ToolsConfig{"shellcheck": {Backend: process}}
				request.Tools = registry.Registrations(registry.Selections{"shellcheck": {Command: "shellcheck"}})
				shell = "bash"
			}
			requestSources = []workspace.Source{{Path: "ci.yml", Config: cfg, Content: []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: " + shell + "\n        run: echo\n" + "\npermissions: {}\n")}}
			result, err := checking.Analyze(t.Context(), request, requestSources)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != name {
				t.Fatalf("invocation lost: %+v", result.Diagnostics)
			}
		})
	}
}
