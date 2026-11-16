package correctness

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/runners"
)

func TestContainerRuleConsumesPreparedUses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		limitation runners.ContainerLimitation
		severity   string
	}{
		{runners.ContainerUnknown, ""},
		{runners.ContainerRequiresLinux, "error"},
		{runners.ContainerMissingDaemon, "warning"},
	} {
		uses := []runners.ContainerUse{{Target: runners.ContainerTarget{Limitation: tc.limitation, Runner: "selected"}, Kind: runners.DockerAction, Name: "docker://alpine", Position: source.Position{Line: 8, Column: 11}}}
		for range 2 {
			findings := ContainerRunnerFindings(uses)
			if tc.severity == "" {
				if len(findings) != 0 {
					t.Fatalf("unknown support produced a finding: %+v", findings)
				}
				continue
			}
			if len(findings) != 1 || findings[0].Severity != tc.severity || findings[0].Start != uses[0].Position || findings[0].Message == "consumer mutation" {
				t.Fatalf("prepared use lost its severity or location: %+v", findings)
			}
			findings[0].Message = "consumer mutation"
		}
	}
}
