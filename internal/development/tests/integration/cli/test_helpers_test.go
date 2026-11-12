package cli_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gitclient "actionlint.kjanat.dev/v2/internal/integrations/git"

	"actionlint.kjanat.dev/v2/internal/cli"
)

type commandTranscript struct {
	Status int    `json:"status"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

func gitRepository(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := gitclient.Command(t.Context(), "git", dir, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
}

func commandTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRepository(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".github", "workflows", "ci.yml"), []byte(commandBadWorkflow), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func workingDirectoryPath(t *testing.T, path string) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(directory, path)
	if err != nil {
		t.Fatal(err)
	}
	return relative
}

const commandGoodWorkflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
permissions: {}
`

const commandBadWorkflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo '${{ missing.value }}'
permissions: {}
`

// testRunCommand disables summaries for diagnostic transcript tests.
func testRunCommand(input string, args ...string) commandTranscript {
	return testRunCommandDefaults(input, append([]string{"--summary=false"}, args...)...)
}

func testRunCommandDefaults(input string, args ...string) commandTranscript {
	var out, errout bytes.Buffer
	cmd := cli.Command{Stdin: strings.NewReader(input), Stdout: &out, Stderr: &errout}
	status := cmd.Main(append([]string{"actionlint", "--no-tool=shellcheck", "--no-tool=ruff", "--no-tool=ruff", "--no-color"}, args...))
	return commandTranscript{status, out.String(), errout.String()}
}

func fmtTestName(modern bool, kind string) string {
	if modern {
		return "modern/" + kind
	}
	return "legacy/" + kind
}

type commandFailingIO struct{}

func (commandFailingIO) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func (commandFailingIO) Write([]byte) (int, error) { return 0, errors.New("write failed") }

type doctorTool struct {
	Name      string   `json:"name"`
	Command   string   `json:"command"`
	Path      string   `json:"path"`
	Arguments []string `json:"arguments"`
	Status    string   `json:"status"`
	Error     string   `json:"error"`
}

type commandBuildInfo struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	InstalledFrom string `json:"installed_from"`
	GoVersion     string `json:"go_version"`
	OS            string `json:"os"`
	GOARCH        string `json:"goarch"`
}

type commandFlagDescription struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Group       string `json:"group"`
	Repeatable  bool   `json:"repeatable"`
}

type commandDescription struct {
	Name     string                   `json:"name"`
	Flags    []commandFlagDescription `json:"flags"`
	Commands []commandDescription     `json:"commands"`
}
