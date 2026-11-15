package correctness

import (
	"testing"

	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
)

func TestRunnerSupportMessagesDescribeTheNextState(t *testing.T) {
	t.Parallel()
	date, err := runnerapi.ParseDate("2027-01-01")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		state, next runnerapi.SupportState
		want        string
	}{
		{runnerapi.Deprecated, runnerapi.Unsupported, "Support for this runner will end by 2027-01-01."},
		{runnerapi.Deprecated, runnerapi.Supported, "Support for this runner is deprecated."},
		{runnerapi.Unsupported, runnerapi.Supported, "Support for this runner is scheduled to start on 2027-01-01."},
	} {
		problem := runnerlang.SupportProblem{Policy: runnerapi.SupportPolicy{Description: "this runner"}, State: tc.state, Next: &runnerapi.SupportChange{On: date, State: tc.next}}
		if got := supportMessage(problem); got != tc.want {
			t.Errorf("support message = %q, want %q", got, tc.want)
		}
	}
}
