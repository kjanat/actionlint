package expressions_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestEventGuardReview(t *testing.T) {
	for _, tc := range []struct {
		name, trigger, condition string
		wantFinding              bool
	}{
		{"folded event", "[push, issues]", "github.event_name == format('pu{0}', 'sh') && github.event.issue.title", true},
		{"reversed folded event", "[push, issues]", "format('pu{0}', 'sh') == github.event_name && github.event.issue.title", true},
		{"matching folded event", "[push, issues]", "github.event_name == format('iss{0}', 'ues') && github.event.issue.title", false},
		{"unreachable conjunction", "push", "github.event_name == 'issues' && github.event.issue.title", false},
		{"unreachable disjunction", "push", "github.event_name == 'push' || github.event.issue.title", false},
		{"unreachable case value", "push", "case(github.event_name == 'issues', github.event.issue.title, false)", false},
		{"reachable missing property", "push", "github.event_name == 'push' && github.event.issue.title", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := repoconfig.Parse([]byte("tools: {shellcheck: false, ruff: false}"))
			if err != nil {
				t.Fatal(err)
			}
			source := "on: " + tc.trigger + "\njobs:\n  test:\n    if: ${{ " + tc.condition + " }}\n    runs-on: ubuntu-latest\n    steps: [{run: echo ok}]\n"
			result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: "workflow.yml", Content: []byte(source), Config: cfg}})
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantFinding {
				if len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, "property \"issue\"") {
					t.Fatalf("missing narrowed payload finding: %+v", result.Diagnostics)
				}
			} else if len(result.Diagnostics) != 0 {
				t.Fatalf("valid guard rejected: %+v", result.Diagnostics)
			}
		})
	}
}
