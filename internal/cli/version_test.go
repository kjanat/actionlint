package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
)

func TestCommandVersionNamesTheModule(t *testing.T) {
	var output bytes.Buffer
	cmd := Command{Stdin: os.Stdin, Stdout: &output, Stderr: &output}

	if status := cmd.Main([]string{"actionlint", "-version"}); status != actionlint.ExitStatusSuccessNoProblem {
		t.Fatal("exit status should be 0 but got", status)
	}

	lines := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("version output should have 3 lines but has %d: %q", len(lines), output.String())
	}
	if !strings.HasPrefix(lines[0], "actionlint.kjanat.dev ") {
		t.Errorf("first line should start with the module path: %q", lines[0])
	}
}
