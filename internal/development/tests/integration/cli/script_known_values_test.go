package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/results/report"
)

// The tar step mirrors actions/upload-pages-artifact@fc324d3547104276b827a68afc52ff2a11cc49c9 action.yml lines 33-42.
func TestKnownValueArgumentsAreNotScriptInterpolation(t *testing.T) {
	t.Parallel()
	const action = `name: pages
description: test
inputs:
  include-hidden-files:
    description: include hidden files
    default: 'false'
runs:
  using: composite
  steps:
    - shell: bash
      run: |
        tar \
          --directory "$INPUT_PATH" \
          -cvf "$RUNNER_TEMP/artifact.tar" \
          --exclude=.git \
          ${{ inputs.include-hidden-files != 'true' && '--exclude=.[^/]*' || '' }} \
          .
    - shell: bash
      run: |
        ${{ inputs.include-hidden-files != 'true' && 'FLAG=value' || 'true' }} tar --version
`
	path := filepath.Join(t.TempDir(), "action.yml")
	if err := os.WriteFile(path, []byte(action), 0o600); err != nil {
		t.Fatal(err)
	}
	got := testRunCommand("", "--no-config", "--json", path)
	var result report.CheckResult
	if err := json.Unmarshal([]byte(got.Stdout), &result); err != nil {
		t.Fatalf("invalid JSON: %v: %+v", err, got)
	}
	var lines []int
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Rule == "script-interpolation" {
			lines = append(lines, diagnostic.Start.Line)
		}
	}
	if len(lines) != 1 || lines[0] != 20 {
		t.Fatalf("script-interpolation lines = %v, want only the command word on line 20: %+v", lines, result.Diagnostics)
	}
}
