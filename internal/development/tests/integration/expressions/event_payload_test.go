package expressions_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestWorkflowEventPayloadChecks(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, trigger, value, want string }{
		{"typo", "pull_request", "github.event.pull_request.titel", `did you mean "title"`},
		{"valid", "pull_request", "github.event.pull_request.title", ""},
		{"different event", "push", "github.event.pull_request.title", `property "pull_request"`},
		{"both events", "[push, pull_request]", "github.event.pull_request.title", ""},
		{"unknown inherited event", "workflow_call", "github.event.future.nested", ""},
		{"dynamic payload", "repository_dispatch", "github.event.client_payload.matrix", ""},
		{"object argument uses runner conversion", "pull_request", "contains(github.event.pull_request, 'x')", ""},
		{"typed number", "pull_request", "github.event.pull_request.number", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			content := "on: " + tc.trigger + "\njobs:\n  test:\n    runs-on: ubuntu-latest\n    env:\n      VALUE: ${{ " + tc.value + " }}\n    steps:\n      - run: echo ok\n" + "\npermissions: {}\n"
			result, err := checking.Analyze(t.Context(), checking.Options{}, []workspace.Source{{Path: "workflow.yaml", Content: []byte(content)}})
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if len(result.Diagnostics) != 0 {
					t.Fatal(result.Diagnostics)
				}
			} else if len(result.Diagnostics) == 0 || !strings.Contains(result.Diagnostics[0].Message, tc.want) {
				t.Fatalf("want %q: %+v", tc.want, result.Diagnostics)
			}
			if tc.want != "" {
				for _, finding := range result.Diagnostics {
					if !strings.Contains(finding.Message, tc.want) {
						t.Fatal(finding)
					}
				}
			}
		})
	}
}
