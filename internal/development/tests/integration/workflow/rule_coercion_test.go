package workflow_test

import (
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

func TestCoercionSelectionAndSuppression(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, config, comment, severity string
		strict                          bool
	}{
		{"default", "", "", "", false},
		{"enabled", "lint: {rules: {suspicious: {coercion: on}}}", "", "warning", false},
		{"error", "lint: {rules: {suspicious: {coercion: error}}}", "", "error", false},
		{"off", "lint: {rules: {suspicious: {coercion: off}}}", "", "", false},
		{"suppressed", "lint: {rules: {suspicious: {coercion: on}}}", " # ignore[coercion]", "", false},
		{"strict", "lint: {rules: {preset: none, suspicious: {coercion: on}}}", "", "warning", true},
		{"strict off", "lint: {rules: {preset: none, suspicious: {coercion: off}}}", "", "", true},
		{"override", "lint: {rules: {suspicious: {coercion: on}}}\noverrides: [{includes: ['test.yaml'], lint: {rules: {suspicious: {coercion: off}}}}]", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := fmt.Sprintf("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - if: startsWith(true, 'TR')%s\n        run: echo ok\n", tc.comment)
			findings := testutil.WorkflowDiagnosticsWithOptions(t, source, tc.config, checking.Options{Presets: repoconfig.Presets{Strict: tc.strict}})
			count := 0
			for _, finding := range findings {
				if finding.Rule == "expression-type" {
					t.Fatalf("valid conversion rejected: %+v", finding)
				}
				if finding.Rule != "coercion" {
					continue
				}
				count++
				if finding.Severity != tc.severity || finding.Start.Line != 6 {
					t.Fatalf("unexpected finding: %+v", finding)
				}
			}
			want := 0
			if tc.severity != "" {
				want = 1
			}
			if count != want {
				t.Fatalf("findings=%+v, want %d coercion findings", findings, want)
			}
		})
	}
}
