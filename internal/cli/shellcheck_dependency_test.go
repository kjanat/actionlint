package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestShellcheckDependencyProcess(t *testing.T) {
	if os.Getenv("ACTIONLINT_SOURCE_HELPER") != "1" {
		return
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		os.Exit(2)
	}
	if err := json.NewEncoder(os.Stdout).Encode(struct {
		Comments []string `json:"comments"`
		Sources  []string `json:"sources"`
	}{Comments: []string{}, Sources: []string{os.Getenv("ACTIONLINT_SOURCE_FILE")}}); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestReportCannotOverwriteCleanShellcheckDependency(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	file := filepath.Join(outside, "clean.sh")
	const original = "VALUE=clean\n"
	if err := os.WriteFile(file, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	workflow := filepath.Join(root, "ci.yml")
	if err := os.WriteFile(workflow, []byte(`on: push
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`), 0o600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "actionlint.yaml")
	contents := fmt.Sprintf(`tools:
  shellcheck:
    backend:
      process:
        executable: %s
        args: ['-test.run=^TestShellcheckDependencyProcess$', '--']
        env:
          ACTIONLINT_SOURCE_HELPER: '1'
          ACTIONLINT_SOURCE_FILE: %s
`, strconv.Quote(executable), strconv.Quote(file))
	if err := os.WriteFile(config, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	command := Command{Stdout: &output, Stderr: &output}
	status := command.MainContext(t.Context(), []string{"actionlint", "check", "--config", config, "--allow-config-processes", "--no-tool=ruff", "--output-file", file, "--yes", workflow})
	actual, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if status != 3 || !strings.Contains(output.String(), "also an input") || string(actual) != original {
		t.Fatalf("exit=%d source=%q output=%s", status, actual, &output)
	}
}
