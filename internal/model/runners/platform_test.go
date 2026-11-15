package runner

import (
	"testing"

	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

func TestRuleShellNameGetPlatformFromXcodeRunner(t *testing.T) {
	t.Parallel()
	testCases := []string{
		"xcode-27",
		"xcode-27-xlarge",
		"Xcode-27",
	}

	for _, label := range testCases {
		t.Run(label, func(t *testing.T) {
			t.Parallel()
			fixture := runnerWorkflow(t, label)
			selection := declaration.RunnerSelection(fixture.Views["test"].RunsOn)
			if got := PlatformOf(selection); got != MacOrLinux {
				t.Fatalf("platform for runner label %q is %v, want %v", label, got, MacOrLinux)
			}
		})
	}
}
