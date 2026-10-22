package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/ruff"
	"actionlint.kjanat.dev/internal/toolconfig"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func TestFindingEqualityRetainsDiagnosticIdentity(t *testing.T) {
	base := Error{Kind: "first", Filepath: "ci.yml", Message: "same", Line: 1, Column: 1}
	for _, change := range []struct {
		name string
		edit func(*Error)
	}{
		{"rule", func(e *Error) { e.Kind = "second" }},
		{"code", func(e *Error) { e.code = "E001" }},
		{"inclusive end", func(e *Error) { e.endColumn = 5 }},
		{"end", func(e *Error) { e.endPosition = &workflowlang.Pos{Line: 2, Col: 1} }},
		{"fix", func(e *Error) { e.fixes = []DiagnosticFix{{Description: "fix"}} }},
	} {
		t.Run(change.name, func(t *testing.T) {
			other := base
			change.edit(&other)
			if equalsErrors(&base, &other) {
				t.Fatalf("distinct %s incorrectly deduplicated", change.name)
			}
		})
	}
}

func TestActionMetadataFailureHasOneOwningRule(t *testing.T) {
	root := t.TempDir()
	writeShellcheckFixture(t, root, "broken/action.yml", "runs: [")
	source := []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: ./broken
        id: broken
      - run: echo '${{ steps.broken.outputs.result }}'
`)
	linter, err := NewLinter(io.Discard, &LinterOptions{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	findings, err := linter.Lint("ci.yml", source, &Project{root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Kind != "action" || findings[0].Line != 6 {
		t.Fatalf("metadata failure should belong to action rule at uses: %v", findings)
	}
}

func TestRuffRetainsInitializationAndCheckFailures(t *testing.T) {
	initial := errors.New("first configuration failed")
	missing := filepath.Join(t.TempDir(), "missing-directory")
	checker := ruff.New(func([]string, string, func([]byte, error) error) { t.Fatal("unexpected execution") }, func() error { return nil }, ruffExpressionEnd)
	checker.WorkingDirectory(missing)
	first := &Config{}
	r := &ruffAnalyzer{checker: checker, forConfig: func(cfg *Config) (*ruff.Checker, error) {
		if cfg == first {
			return nil, initial
		}
		return checker, nil
	}}
	pos := &workflowlang.Pos{Line: 1, Col: 1}
	run := &workflowlang.ExecRun{Run: &workflowlang.String{Value: "print(1)", Pos: pos}, RunPos: pos}
	python := "python"
	_, err := r.AnalyzeScripts(t.Context(), scriptBatch{scripts: []scriptUnit{
		{run: run, shell: &python, config: first}, {run: run, shell: &python},
	}})
	if !errors.Is(err, initial) || !strings.Contains(err.Error(), missing) {
		t.Fatalf("lost an incomplete-analysis cause: %v", err)
	}
}

type countShellcheckBackend struct{ calls int }

func (b *countShellcheckBackend) Check(context.Context, analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	b.calls++
	return nil, nil
}

func TestNestedCompositeUsesItsOwnShellcheckOverride(t *testing.T) {
	root := t.TempDir()
	writeShellcheckFixture(t, root, "inner/action.yml", "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo inner\n")
	writeShellcheckFixture(t, root, "outer/action.yml", "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: $/inner\n")
	config, err := ParseConfig([]byte("tools: {shellcheck: true, ruff: false}\noverrides:\n  - includes: ['inner/action.yml']\n    tools: {shellcheck: false}\n"))
	if err != nil {
		t.Fatal(err)
	}
	backend := &countShellcheckBackend{}
	_, err = Analyze(t.Context(), AnalysisRequest{
		WorkingDir: root, ShellcheckBackend: backend,
		Sources: []SourceUnit{{Path: "workflow.yml", Config: config, Project: &Project{root: root},
			Content: []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: $/outer\n")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if backend.calls != 0 {
		t.Fatalf("disabled inner action reached ShellCheck %d times", backend.calls)
	}
}

func TestRepositoryProcessHelper(t *testing.T) {
	if os.Getenv("ACTIONLINT_REPOSITORY_PROCESS") != "1" {
		return
	}
	if err := os.WriteFile(os.Getenv("ACTIONLINT_REPOSITORY_PROCESS_MARKER"), []byte("executed"), 0o600); err != nil {
		os.Exit(2)
	}
	if os.Getenv("ACTIONLINT_REPOSITORY_TOOL") == "shellcheck" {
		fmt.Print(`{"comments":[]}`)
	} else {
		fmt.Print(`[]`)
	}
	os.Exit(0)
}

func TestRepositorySelectedProcessRequiresHostOptIn(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"ruff", "shellcheck"} {
		t.Run(tool, func(t *testing.T) {
			root := t.TempDir()
			marker := filepath.Join(root, "executed")
			backend := &toolconfig.Backend{Process: &toolconfig.Command{
				Executable: executable,
				Args:       []string{"-test.run=^TestRepositoryProcessHelper$", "--"},
				Env:        map[string]string{"ACTIONLINT_REPOSITORY_PROCESS": "1", "ACTIONLINT_REPOSITORY_PROCESS_MARKER": marker, "ACTIONLINT_REPOSITORY_TOOL": tool},
			}}
			cfg := &Config{}
			request := AnalysisRequest{WorkingDir: root}
			shell := "python"
			if tool == "ruff" {
				cfg.Tools.Ruff.Backend = backend
				request.Ruff = "ruff"
			} else {
				cfg.Tools.Shellcheck.Backend = backend
				request.ShellCheck = "shellcheck"
				shell = "bash"
			}
			request.Sources = []SourceUnit{{Path: "ci.yml", Config: cfg, Content: []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: " + shell + "\n        run: echo\n")}}
			_, err := Analyze(t.Context(), request)
			if err == nil || !strings.Contains(err.Error(), "--allow-config-processes") {
				t.Errorf("repository process selection needs an actionable opt-in error: %v", err)
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("repository-selected executable ran without host approval: %v", err)
			}
			request.AllowConfigProcesses = true
			if _, err := Analyze(t.Context(), request); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("explicit host opt-in did not run configured process: %v", err)
			}
		})
	}
}
