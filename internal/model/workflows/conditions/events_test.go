package conditions

import (
	"slices"
	"testing"
)

func TestEventReachabilityUsesPartialContextKnowledge(t *testing.T) {
	t.Parallel()
	events := []string{"push", "pull_request", "pull_request_target"}
	for _, tc := range []struct {
		condition string
		want      []string
	}{
		{`contains(fromJSON('["push", "pull_request"]'), github.event_name)`, []string{"push", "pull_request"}},
		{"contains(github.event_name, 'pull_request')", []string{"pull_request", "pull_request_target"}},
		{"github['event_name'] == 'push'", []string{"push"}},
		{"GITHUB.EVENT_NAME == 'PUSH'", []string{"push"}},
		{"inputs.allowed && github.event_name == 'push'", []string{"push"}},
		{"inputs.allowed || github.event_name == 'push'", events},
		{"success() && github.event_name == 'push'", []string{"push"}},
		{"failure() || github.event_name == 'push'", events},
		{"github.actor == ''", events},
		{"github.event_name ==", events},
		{"prefix ${{ false }}", events},
		{"${{ false }} suffix", events},
		{"${{ false }}${{ false }}", events},
		{" ${{ false }}", events},
		{"${{ false }} ", events},
		{"false", nil},
		{"", events},
	} {
		t.Run(tc.condition, func(t *testing.T) {
			t.Parallel()
			got := ReachableEvents(events, tc.condition)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("events=%v, want %v", got, tc.want)
			}
			if len(got) > 0 {
				got[0] = "consumer mutation"
				if events[0] != "push" {
					t.Fatal("reachability result mutated input events")
				}
			}
		})
	}
}
