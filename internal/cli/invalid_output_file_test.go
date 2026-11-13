package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	resultreport "actionlint.kjanat.dev/v2/internal/results/report"
)

func TestInvalidOutputFileThroughCLI(t *testing.T) {
	t.Parallel()
	// Test the output-writing step from comment 6710252262 on its own.
	const source = `name: Output file regression
description: Check the producer independently
runs:
  using: composite
  steps:
    - shell: bash
      run: |
        tee <<EOF2 >"${GITHUB_OUTPUT}"
        {
          "available": true
        }
        EOF2
`
	root := t.TempDir()
	path := filepath.Join(root, "action.yml")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "actionlint.yaml")
	if err := os.WriteFile(configPath, []byte("tools: {shellcheck: false, ruff: false}\nlint: {rules: {preset: none, correctness: {invalid-output-file: error}}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output, log bytes.Buffer
	command := Command{Stdin: strings.NewReader(""), Stdout: &output, Stderr: &log}
	status := command.MainContext(t.Context(), []string{"actionlint", "check", "--config", configPath, "--output-format=json", "--summary=false", path})
	if status != 1 {
		t.Fatalf("status=%d, output=%s, stderr=%s", status, output.String(), log.String())
	}
	var report resultreport.CheckResult
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Diagnostics) != 1 {
		t.Fatalf("expected one producer diagnostic: %+v", report.Diagnostics)
	}
	finding := report.Diagnostics[0]
	if finding.Rule != "invalid-output-file" || finding.Severity != "error" || finding.Start.Line != 8 || finding.Start.Column != 21 || finding.End.Line != 8 || finding.End.Column != 39 {
		t.Fatalf("unexpected producer diagnostic: %+v", finding)
	}
	line := strings.Split(source, "\n")[finding.Start.Line-1]
	if token := line[finding.Start.Column-1 : finding.End.Column-1]; token != `"${GITHUB_OUTPUT}"` {
		t.Fatalf("destination range = %q", token)
	}
}
