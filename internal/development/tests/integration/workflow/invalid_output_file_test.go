package workflow_test

import (
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	"actionlint.kjanat.dev/v2/internal/results"
)

const outputFileWorkflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: bash
        run: |
          tee <<EOF2 >"${GITHUB_OUTPUT}"
          {
            "available": true
          }
          EOF2
`

func TestInvalidOutputFileSelectionAndSuppression(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, config, comment, severity string
	}{
		{"default", "", "", "error"},
		{"disabled", "lint: {rules: {correctness: {invalid-output-file: off}}}", "", ""},
		{"warning", "lint: {rules: {correctness: {invalid-output-file: warn}}}", "", "warning"},
		{"suppressed", "", " # ignore[invalid-output-file]", ""},
		{"unrelated suppression", "", " # ignore[deprecated-commands]", "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := strings.Replace(outputFileWorkflow, "run: |", "run: |"+tc.comment, 1)
			findings := onlyOutputFileFindings(testutil.WorkflowDiagnostics(t, source, tc.config))
			if tc.severity == "" {
				if len(findings) != 0 {
					t.Fatalf("unexpected output-file findings: %+v", findings)
				}
				return
			}
			if len(findings) != 1 || findings[0].Severity != tc.severity || findings[0].Start.Line != 8 || findings[0].Start.Column != 23 || findings[0].End.Line != 8 || findings[0].End.Column != 41 {
				t.Fatalf("expected one %s on the destination word at 8:23-41: %+v", tc.severity, findings)
			}
			if !strings.Contains(findings[0].Message, `line "{"`) || len(findings[0].Fixes) != 0 {
				t.Fatalf("unexpected diagnostic: %+v", findings[0])
			}
		})
	}
}

func TestInvalidOutputFileValidAndUnknown(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, script, shell string }{
		{"assignment", `echo 'available=true' >> "$GITHUB_OUTPUT"`, "bash"},
		{"partial assignment", "printf 'available=' >> \"$GITHUB_OUTPUT\"\nprintf '%s\\n' true >> \"$GITHUB_OUTPUT\"", "bash"},
		{"multiline JSON", "{ echo 'available<<EOF'; echo '{'; echo '}'; echo EOF; } >> \"$GITHUB_OUTPUT\"", "bash"},
		{"dynamic", `echo "$VALUE" >> "$GITHUB_OUTPUT"`, "bash"},
		{"unknown prefix", "generate >> \"$GITHUB_OUTPUT\"\necho '{' >> \"$GITHUB_OUTPUT\"", "bash"},
		{"unknown shell", `echo '{' >> "$GITHUB_OUTPUT"`, "${{ inputs.shell }}"},
		{"PowerShell", `echo '{' >> "$GITHUB_OUTPUT"`, "pwsh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := fmt.Sprintf("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: %s\n        run: |\n          %s\n", tc.shell, strings.ReplaceAll(tc.script, "\n", "\n          "))
			if findings := onlyOutputFileFindings(testutil.WorkflowDiagnosticsWithOptions(t, source, "", checking.Options{})); len(findings) != 0 {
				t.Fatalf("valid or unknown write reported: %+v", findings)
			}
		})
	}
}

func onlyOutputFileFindings(findings []results.Diagnostic) []results.Diagnostic {
	var matches []results.Diagnostic
	for _, finding := range findings {
		if finding.Rule == "invalid-output-file" {
			matches = append(matches, finding)
		}
	}
	return matches
}
