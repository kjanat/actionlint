package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/config"
	toolconfig "actionlint.kjanat.dev/v2/internal/config/tools"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func writeReviewFixture(t *testing.T, root, path, content string) {
	t.Helper()
	file := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestActionMetadataFailureHasOneOwningRule(t *testing.T) {
	root := t.TempDir()
	writeReviewFixture(t, root, "broken/action.yml", "runs: [")
	source := []byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: ./broken
        id: broken
      - env: {RESULT: '${{ steps.broken.outputs.result }}'}
        run: echo "$RESULT"
`)
	result, err := runTestAnalysis(t.Context(), analysisRequest{WorkingDir: root, Sources: []sourceUnit{{Path: "ci.yml", Content: source, Project: workspace.Identity(root)}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "action" || result.Diagnostics[0].Start.Line != 6 {
		t.Fatalf("metadata failure should belong to action rule at uses: %v", result.Diagnostics)
	}
}

type reviewShellcheckBackend struct{ calls int }

func (b *reviewShellcheckBackend) Check(context.Context, analyzer.ScriptRequest) ([]analyzer.ScriptDiagnostic, error) {
	b.calls++
	return nil, nil
}

func TestNestedCompositeUsesItsOwnShellcheckOverride(t *testing.T) {
	root := t.TempDir()
	writeReviewFixture(t, root, "inner/action.yml", "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo inner\n")
	writeReviewFixture(t, root, "outer/action.yml", "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: $/inner\n")
	cfg, err := config.Parse([]byte("tools: {shellcheck: true, ruff: false}\noverrides:\n  - includes: ['inner/action.yml']\n    tools: {shellcheck: false}\n"))
	if err != nil {
		t.Fatal(err)
	}
	backend := &reviewShellcheckBackend{}
	_, err = runTestAnalysis(t.Context(), analysisRequest{
		WorkingDir: root, Tools: (adapters.Options{ShellcheckBackend: backend}).Registrations(),
		Sources: []sourceUnit{{Path: "workflow.yml", Config: cfg, Project: workspace.Identity(root),
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
			cfg := &config.Config{}
			options := adapters.Options{}
			shell := "python"
			if tool == "ruff" {
				cfg.Tools.Ruff.Backend, options.Ruff = backend, "ruff"
			} else {
				cfg.Tools.Shellcheck.Backend, options.ShellCheck = backend, "shellcheck"
				shell = "bash"
			}
			request := analysisRequest{WorkingDir: root, Tools: options.Registrations(), Sources: []sourceUnit{{Path: "ci.yml", Config: cfg,
				Content: []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: " + shell + "\n        run: echo\n")}}}
			_, err := runTestAnalysis(t.Context(), request)
			if err == nil || !strings.Contains(err.Error(), "--allow-config-processes") {
				t.Errorf("repository process selection needs an actionable opt-in error: %v", err)
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("repository-selected executable ran without host approval: %v", err)
			}
			request.AllowConfigProcesses = true
			if _, err := runTestAnalysis(t.Context(), request); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("explicit host opt-in did not run configured process: %v", err)
			}
		})
	}
}
