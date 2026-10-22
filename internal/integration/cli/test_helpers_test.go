package cli_test

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/cli"
	"actionlint.kjanat.dev/v2/internal/workspace"
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
	if out, err := workspace.GitCommand(t.Context(), "git", dir, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
}

func commandTestRepo(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	gitRepository(t, ".")
	if err := os.MkdirAll(".github/workflows", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".github/workflows/ci.yml", []byte(commandBadWorkflow), 0o600); err != nil {
		t.Fatal(err)
	}
}

const commandGoodWorkflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`

const commandBadWorkflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo '${{ missing.value }}'
`

// testRunCommand disables summaries for diagnostic transcript tests.
func testRunCommand(input string, args ...string) commandTranscript {
	return testRunCommandDefaults(input, append([]string{"--summary=false"}, args...)...)
}

func testRunCommandDefaults(input string, args ...string) commandTranscript {
	var out, errout bytes.Buffer
	cmd := cli.Command{Stdin: strings.NewReader(input), Stdout: &out, Stderr: &errout}
	status := cmd.Main(append([]string{"actionlint", "--shellcheck=", "--ruff=", "--pyflakes=", "--no-color"}, args...))
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
