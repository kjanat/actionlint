package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/cli"
	"actionlint.kjanat.dev/internal/render"
)

func TestShellcheckCommandFindingsWithExpressionError(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	command := fmt.Sprintf(`"%s" -test.run=^TestShellcheckFindingsHelper$ -- -x -o all`, filepath.ToSlash(executable))
	const workflow = `on: push
jobs:
  check:
    runs-on: ubuntu-latest
    env: { REPOSITORY_URL: "${{ github.repositoryUsrl }}" }
    steps:
      - run: echo $VALUE
`
	for _, modern := range []bool{false, true} {
		t.Run(fmt.Sprintf("modern=%t", modern), func(t *testing.T) {
			args := []string{"actionlint", "--no-config", "--no-color", "--no-ruff", "--shellcheck=" + command, "--json", "--summary=false"}
			if modern {
				args = append(args, "check")
			}
			var stdout, stderr bytes.Buffer
			cmd := cli.Command{Stdin: strings.NewReader(workflow), Stdout: &stdout, Stderr: &stderr}
			status := cmd.Main(append(args, "-"))
			if status != 1 || stderr.Len() != 0 {
				t.Fatalf("exit=%d, stdout=%s, stderr=%s", status, &stdout, &stderr)
			}
			var result render.CheckResult
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatalf("decode findings: %v: %s", err, &stdout)
			}
			var expression, shellcheck bool
			for _, finding := range result.Diagnostics {
				expression = expression || strings.Contains(strings.ToLower(finding.Message), "repositoryusrl")
				shellcheck = shellcheck || finding.Rule == "shellcheck" && finding.Code == "SC2086"
			}
			if !expression || !shellcheck {
				t.Fatalf("expression and ShellCheck findings must coexist: %s", &stdout)
			}
		})
	}
}

func TestShellcheckFindingsHelper(t *testing.T) {
	separator := slices.Index(os.Args, "--")
	if separator == -1 {
		return
	}
	args := os.Args[separator+1:]
	if len(args) < 3 || !slices.Equal(args[:3], []string{"-x", "-o", "all"}) {
		t.Fatalf("explicit ShellCheck arguments lost: %q", args)
	}
	format := slices.Index(args, "-f")
	if format == -1 || format+1 >= len(args) || args[format+1] != "json1" {
		t.Fatalf("ShellCheck JSON format missing: %q", args)
	}
	script, err := io.ReadAll(os.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(script), "\n") {
		if line == "echo $VALUE" {
			fmt.Fprintf(os.Stdout, `{"comments":[{"file":"-","line":%d,"endLine":%d,"column":6,"endColumn":12,"level":"info","code":2086,"message":"Double quote to prevent globbing and word splitting."}]}`, i+1, i+1)
			os.Exit(1)
		}
	}
	t.Fatalf("run block missing from ShellCheck input: %q", script)
}
