package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

type commandTranscript struct {
	Status int    `json:"status"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

func gitRepository(t *testing.T, dir string) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", "init", "--quiet", dir)
	for _, entry := range os.Environ() {
		if name, _, _ := strings.Cut(entry, "="); !strings.HasPrefix(name, "GIT_") {
			command.Env = append(command.Env, entry)
		}
	}
	if out, err := command.CombinedOutput(); err != nil {
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

// Most transcript tests focus on diagnostics rather than the completion summary.
func testRunCommand(input string, args ...string) commandTranscript {
	return testRunCommandDefaults(input, append([]string{"--summary=false"}, args...)...)
}

func testRunCommandDefaults(input string, args ...string) commandTranscript {
	var out, errout bytes.Buffer
	cmd := Command{Stdin: strings.NewReader(input), Stdout: &out, Stderr: &errout}
	status := cmd.Main(append([]string{"actionlint", "--shellcheck=", "--ruff=", "--pyflakes=", "--no-color"}, args...))
	return commandTranscript{status, out.String(), errout.String()}
}

type commandFailingIO struct{}

func (commandFailingIO) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func (commandFailingIO) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func fmtTestName(modern bool, kind string) string {
	if modern {
		return "modern/" + kind
	}
	return "legacy/" + kind
}
