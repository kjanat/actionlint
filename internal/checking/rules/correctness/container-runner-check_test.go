package correctness

import (
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
)

func TestContainerRuleConsumesPreparedUses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		limitation runnerlang.ContainerLimitation
		severity   string
	}{
		{runnerlang.ContainerUnknown, ""},
		{runnerlang.ContainerRequiresLinux, "error"},
		{runnerlang.ContainerMissingDaemon, "warning"},
	} {
		uses := []runnerlang.ContainerUse{{Target: runnerlang.ContainerTarget{Limitation: tc.limitation, Runner: "selected"}, Kind: runnerlang.DockerAction, Name: "docker://alpine", Position: sourcepos.Position{Line: 8, Column: 11}}}
		for range 2 {
			findings := ContainerRunnerFindings(uses)
			if tc.severity == "" {
				if len(findings) != 0 {
					t.Fatalf("unknown support produced a finding: %+v", findings)
				}
				continue
			}
			if len(findings) != 1 || findings[0].Rule != "container-runner" || findings[0].Severity != tc.severity || findings[0].Start != uses[0].Position || findings[0].Message == "consumer mutation" {
				t.Fatalf("prepared use lost its severity or location: %+v", findings)
			}
			findings[0].Message = "consumer mutation"
		}
	}
}
