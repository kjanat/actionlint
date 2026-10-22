package application

import (
	"actionlint.kjanat.dev/internal/config"
	toolconfig "actionlint.kjanat.dev/internal/config/tools"
	"actionlint.kjanat.dev/internal/tools/adapters"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

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
	cfg, err := config.Load(child)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Tools.Ruff.Backend.Process.Executable; got != filepath.Join(parentDir, "bin", "ruff") {
		t.Fatal(got)
	}
	effective, err := config.ForFile(cfg, filepath.Join(root, "ci.yml"), root)
	if err != nil {
		t.Fatal(err)
	}
	if got := effective.Tools.Shellcheck.Backend.Process.Executable; got != filepath.Join(parentDir, "bin", "shellcheck") {
		t.Fatal(got)
	}
}

type backendPosition struct {
	Row    int `json:"row"`
	Column int `json:"column"`
}

type backendFinding struct {
	Filename    string          `json:"filename"`
	Code        string          `json:"code"`
	Message     string          `json:"message"`
	Location    backendPosition `json:"location"`
	EndLocation backendPosition `json:"end_location"`
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
		encoded, _ := json.Marshal([]backendFinding{{Filename: filepath.Join(cwd, "actionlint.py"), Code: "F821", Message: "backend process finding", Location: backendPosition{1, 1}, EndLocation: backendPosition{1, 2}}})
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
			cfg := &config.Config{}
			request := analysisRequest{WorkingDir: t.TempDir(), AllowConfigProcesses: true}
			options := adapters.Options{}
			shell := "python"
			if name == "ruff" {
				cfg.Tools.Ruff.Backend = process
				options.Ruff = "ruff"
			} else {
				cfg.Tools.Shellcheck.Backend = process
				options.ShellCheck = "shellcheck"
				shell = "bash"
			}
			request.Sources = []sourceUnit{{Path: "ci.yml", Config: cfg, Content: []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: " + shell + "\n        run: echo\n")}}
			request.Tools = options.Registrations()
			result, err := runTestAnalysis(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != name {
				t.Fatalf("invocation lost: %+v", result.Diagnostics)
			}
		})
	}
}
