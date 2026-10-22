// Package testutil provides source and filesystem fixtures for integration tests.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

const (
	GoodWorkflow    = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
	BadWorkflow     = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo '${{ missing.value }}'\n"
	WorkflowSteps   = "    runs-on: ubuntu-latest\n    steps:\n      - run: echo hello\n"
	CompositeAction = "name: example\ndescription: example\nruns:\n  using: composite\n  steps:\n    - run: echo ok\n      shell: bash\n"
)

func WriteFixture(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func ExecutableFixture(t *testing.T) (string, func(...string)) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git required")
	}
	root := t.TempDir()
	command := func(args ...string) {
		t.Helper()
		if out, err := workspace.GitCommand(t.Context(), git, root, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	command("init", "--quiet")
	command("config", "core.filemode", "false")
	for _, name := range []string{"bad.sh", "good.sh", "scripts/bad.sh", "scripts/good.sh", "space dir/bad.sh", "{good,bad}.sh"} {
		WriteFixture(t, root, name, "#!/bin/sh\necho script\n")
		command("add", "--", name)
		mode := "-x"
		if strings.HasSuffix(name, "good.sh") {
			mode = "+x"
		}
		command("update-index", "--chmod="+mode, "--", name)
	}
	WriteFixture(t, root, "untracked.sh", "#!/bin/sh\n")
	WriteFixture(t, root, "intent.sh", "#!/bin/sh\n")
	command("add", "-N", "intent.sh")
	WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false}\n")
	return root, command
}

func RequireShellcheck(t *testing.T) string {
	t.Helper()
	command, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skip("ShellCheck required")
	}
	t.Setenv("SHELLCHECK_OPTS", "")
	return command
}

func RequireRuff(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("ruff")
	if err != nil {
		t.Skip("Ruff is not installed")
	}
	return path
}

func AssertSourceFile(t *testing.T, got, want string) {
	t.Helper()
	actual, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(actual, expected) {
		t.Fatalf("wrong sourced file: got %q, want %q", got, want)
	}
}

func FixtureSources(t testing.TB, paths []string, project *workspace.Project, cfg *repoconfig.Config) []workspace.Source {
	t.Helper()
	if cfg == nil && project != nil {
		cfg = project.Config()
	}
	sources := make([]workspace.Source, 0, len(paths))
	reader := fsread.New(t.Context(), nil, nil)
	for _, path := range paths {
		content, err := reader.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		display := path
		if project != nil && filepath.IsAbs(path) {
			if relative, err := filepath.Rel(project.RootDir(), path); err == nil {
				display = relative
			}
		}
		sources = append(sources, workspace.Source{Path: display, InputPath: path, Content: content, Project: project, Config: cfg})
	}
	return sources
}

// Main gives file-based suites a stable repository root and isolated Git settings.
func Main(m *testing.M) {
	if err := os.Chdir(filepath.Join("..", "..", "..")); err != nil {
		panic(err)
	}
	directory, err := os.MkdirTemp("", "actionlint-test-config-")
	if err != nil {
		panic(err)
	}
	global := filepath.Join(directory, "gitconfig")
	if err := os.WriteFile(global, nil, 0600); err != nil {
		panic(err)
	}
	for name, value := range map[string]string{"GIT_CONFIG_GLOBAL": global, "GIT_CONFIG_NOSYSTEM": "1", "XDG_CONFIG_HOME": directory} {
		if err := os.Setenv(name, value); err != nil {
			panic(err)
		}
	}
	code := m.Run()
	_ = os.RemoveAll(directory)
	os.Exit(code)
}

// Diagnostics projects completed report values without exposing traversal state.
func Diagnostics(result *application.Result, err error) ([]diagnostic.Diagnostic, error) {
	if err != nil {
		return nil, err
	}
	return result.Diagnostics, nil
}

// WorkflowDiagnostics checks a fixture with an explicit in-memory configuration.
func WorkflowDiagnostics(t *testing.T, source, config string) []diagnostic.Diagnostic {
	t.Helper()
	var cfg *repoconfig.Config
	if config != "" {
		var err error
		cfg, err = repoconfig.Parse([]byte(config))
		if err != nil {
			t.Fatal(err)
		}
	}
	result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: "test.yaml", InputPath: "test.yaml", Content: []byte(source), Config: cfg}})
	if err != nil {
		t.Fatal(err)
	}
	return result.Diagnostics
}

// Composite writes a caller fixture and checks it through application input selection.
func Composite(t *testing.T, root, steps string, options application.Options) *application.Result {
	t.Helper()
	workflow := WriteFixture(t, root, ".github/workflows/composite.yml", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    defaults:
      run:
        shell: python
        working-directory: scripts
    steps:
      `+strings.ReplaceAll(steps, "\n", "\n      ")+"\n")
	options.WorkingDir = root
	result, err := application.Check(t.Context(), options, application.Input{Kind: application.Files, Paths: []string{workflow}})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
