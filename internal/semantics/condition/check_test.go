package condition

import (
	"testing"

	"actionlint.kjanat.dev/diagnostic"
)

func TestCheck(t *testing.T) {
	start := diagnostic.Position{Line: 3, Column: 9}
	for _, test := range []struct {
		value, message string
	}{
		{"${{ github.event_name == 'push' }}", ""},
		{"github.event_name == 'push'", ""},
		{"${{ unknown.context }}", ""},
		{"${{ true }} extra", `if: condition "${{ true }} extra" is always evaluated to true because extra characters are around ${{ }}`},
		{"${{ true }}", `constant expression "true" in condition is always truthy. remove the if: section`},
		{"${{ 'false' }}", `constant expression "false" in condition is always falsy. this step or job will be skipped`},
		{"false", `constant expression "false" in condition is always falsy. this step or job will be skipped`},
		{"${{ 0 }}", `constant expression "0" in condition is always falsy. this step or job will be skipped`},
	} {
		t.Run(test.value, func(t *testing.T) {
			findings := Check(test.value, start)
			if test.message == "" {
				if len(findings) != 0 {
					t.Fatalf("unexpected findings: %+v", findings)
				}
				return
			}
			if len(findings) != 1 {
				t.Fatalf("findings: %+v", findings)
			}
			if findings[0].Rule != "if-cond" || findings[0].Start != start || findings[0].Message != test.message {
				t.Fatalf("finding: %+v", findings[0])
			}
		})
	}
}
