package suppression_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestReusableWorkflowJobUsesSuppressions(t *testing.T) {
	for _, uses := range []string{"invalid", "${{ 'invalid' }}"} {
		for _, tc := range []struct {
			name, config, directive string
			want                    int
		}{
			{"unsuppressed", "", "", 1},
			{"configured uses", "ignores: [{rule: workflow-call, uses: invalid}]", "", 0},
			{"different configured uses", "ignores: [{rule: workflow-call, uses: different}]", "", 1},
			{"inline uses line", "ignores: [{rule: workflow-call, uses: different}]", " # actionlint:ignore workflow-call -- reviewed invalid reference", 0},
		} {
			t.Run(uses+"/"+tc.name, func(t *testing.T) {
				source := "on: push\njobs:\n  caller:\n    uses: " + uses + tc.directive + "\n"
				findings := testutil.WorkflowDiagnostics(t, source, tc.config)
				if len(findings) != tc.want {
					t.Fatalf("job uses suppression: got %v; want %d findings", findings, tc.want)
				}
				if tc.want != 0 && (findings[0].Rule != "workflow-call" || !strings.Contains(findings[0].Message, `reusable workflow call "invalid"`)) {
					t.Fatalf("unexpected unsuppressed job finding: %v", findings)
				}
			})
		}
	}
}
