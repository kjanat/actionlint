package application_test

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/workspace"
)

const (
	goodWorkflow    = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
	compositeAction = "name: example\ndescription: example\nruns:\n  using: composite\n  steps:\n    - run: echo ok\n      shell: bash\n"
)

func writeFixture(t *testing.T, root, name, content string) string {
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

func writeGitRepository(t *testing.T, root string) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", "init", "--quiet", root)
	for _, entry := range os.Environ() {
		if name, _, _ := strings.Cut(entry, "="); !strings.HasPrefix(name, "GIT_") {
			command.Env = append(command.Env, entry)
		}
	}
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
}

func TestShellcheckPlanningHelper(t *testing.T) {
	index := -1
	for i, arg := range os.Args {
		if arg == "--" {
			index = i + 1
			break
		}
	}
	if index < 0 || index >= len(os.Args) {
		return
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatal(err)
	}
	switch os.Args[index] {
	case "shellcheck":
		fmt.Fprintln(os.Stdout, `{"comments":[{"line":2,"endLine":2,"column":1,"endColumn":2,"level":"warning","code":9999,"message":"shellcheck invoked"}]}`)
	default:
		t.Fatalf("unexpected helper %q", os.Args[index])
	}
	os.Exit(0)
}

func TestRuffPlanningHelper(t *testing.T) {
	if os.Getenv("ACTIONLINT_RUFF_PLANNING_HELPER") != "1" {
		return
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(os.Stdout, "[]")
	os.Exit(0)
}

const badWorkflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo '${{ missing.value }}'\n"

func executableFixture(t *testing.T) (string, func(...string)) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git required")
	}
	root := t.TempDir()
	configRoot := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", writeFixture(t, configRoot, "gitconfig", ""))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	command := func(args ...string) {
		t.Helper()
		if out, err := workspace.GitCommand(t.Context(), git, root, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	command("init", "--quiet")
	command("config", "core.filemode", "false")
	for _, name := range []string{"bad.sh", "good.sh", "scripts/bad.sh", "scripts/good.sh", "space dir/bad.sh", "{good,bad}.sh"} {
		writeFixture(t, root, name, "#!/bin/sh\necho script\n")
		command("add", "--", name)
		mode := "-x"
		if strings.HasSuffix(name, "good.sh") {
			mode = "+x"
		}
		command("update-index", "--chmod="+mode, "--", name)
	}
	writeFixture(t, root, "untracked.sh", "#!/bin/sh\n")
	writeFixture(t, root, "intent.sh", "#!/bin/sh\n")
	command("add", "-N", "intent.sh")
	writeFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false}\n")
	return root, command
}

func assertShellcheckSourceFile(t *testing.T, got, want string) {
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

func compositeAnalysis(t *testing.T, root, steps string, options application.Options) *application.Result {
	t.Helper()
	workflow := writeFixture(t, root, ".github/workflows/composite.yml", `on: push
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
	session := options
	var err error

	result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func shellcheckForTest(t *testing.T) string {
	t.Helper()
	command, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skip("ShellCheck required")
	}
	t.Setenv("SHELLCHECK_OPTS", "")
	return command
}
