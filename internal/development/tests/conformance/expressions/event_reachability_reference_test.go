package expressions

import (
	"slices"
	"testing"

	workflowconditions "actionlint.kjanat.dev/v2/internal/model/workflows/conditions"
)

func TestEventReachabilityReference(t *testing.T) {
	t.Parallel()
	events := []string{"push", "pull_request", "pull_request_target"}
	for _, tc := range []struct {
		expression string
		want       []string
	}{
		{`contains(fromJSON('["push", "pull_request"]'), github.event_name)`, []string{"push", "pull_request"}},
		{"contains(github.event_name, 'pull_request')", []string{"pull_request", "pull_request_target"}},
		{"github['event_name'] == 'push'", []string{"push"}},
	} {
		for _, event := range events {
			t.Run(tc.expression+"/"+event, func(t *testing.T) {
				t.Parallel()
				want := slices.Contains(tc.want, event)
				got := workflowconditions.ReachableEvents([]string{event}, tc.expression)
				if (len(got) != 0) != want {
					t.Fatalf("event reachability=%v, want %t", got, want)
				}
				CheckValue(t, tc.expression, map[string]any{"github": map[string]any{"event_name": event}}, want)
			})
		}
	}
}
