package actionlint

import "testing"

func TestRuleShellNameGetPlatformFromXcodeRunner(t *testing.T) {
	testCases := []string{
		"xcode-27",
		"xcode-27-xlarge",
		"Xcode-27",
	}

	rule := NewRuleShellName()
	for _, label := range testCases {
		t.Run(label, func(t *testing.T) {
			runner := &Runner{Labels: []*String{{Value: label}}}
			if got := rule.getPlatformFromRunner(runner); got != platformKindMacOrLinux {
				t.Fatalf("platform for runner label %q is %v, want %v", label, got, platformKindMacOrLinux)
			}
		})
	}
}
