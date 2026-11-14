package cli_test

import (
	"encoding/json"
	"testing"

	resultreport "actionlint.kjanat.dev/v2/internal/results/report"
)

// GitHub rejected the empty steps list in https://github.com/kjanat/beepboop/actions/runs/43332264931.
// Runny accepts that list; this test covers GitHub's additional server validation.
func TestEmptyJobDefaultSeverity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, steps string
		invalid     bool
	}{
		{"missing steps", "", true},
		{"empty steps", "    steps: []\n", true},
		{"null script", "    steps: [{run: null}]\n", false},
		{"script", "    steps: [{run: 'echo ok'}]\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := "on: push\npermissions: {}\njobs:\n  wait:\n    runs-on: ubuntu-latest\n" + tc.steps
			got := testRunCommand(source, "check", "--no-config", "--json", "-")
			var result resultreport.CheckResult
			if err := json.Unmarshal([]byte(got.Stdout), &result); err != nil || got.Status > 1 {
				t.Fatalf("check: %+v, %v", got, err)
			}
			found := false
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule == "empty-job" {
					found = true
					if diagnostic.Severity != "error" {
						t.Errorf("empty-job severity = %q, want error", diagnostic.Severity)
					}
				}
			}
			if found != tc.invalid {
				t.Fatalf("empty-job reported = %t, want %t; diagnostics: %+v", found, tc.invalid, result.Diagnostics)
			}
		})
	}
}
