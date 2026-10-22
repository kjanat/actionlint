package runner

import (
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func TestRuleShellNameGetPlatformFromXcodeRunner(t *testing.T) {
	testCases := []string{
		"xcode-27",
		"xcode-27-xlarge",
		"Xcode-27",
	}

	for _, label := range testCases {
		t.Run(label, func(t *testing.T) {
			runner := &workflowlang.Runner{Labels: []*workflowlang.String{{Value: label}}}
			if got := PlatformOf(runner); got != MacOrLinux {
				t.Fatalf("platform for runner label %q is %v, want %v", label, got, MacOrLinux)
			}
		})
	}
}
