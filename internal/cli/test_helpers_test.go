package cli

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

type commandTranscript struct {
	Status int    `json:"status"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

func commandTestRepo(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, path := range []string{".git", ".github/workflows"} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
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

func testRunCommand(input string, args ...string) commandTranscript {
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
