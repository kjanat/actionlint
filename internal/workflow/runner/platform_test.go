package runner

import "testing"

func TestRuleShellNameGetPlatformFromXcodeRunner(t *testing.T) {
	testCases := []string{
		"xcode-27",
		"xcode-27-xlarge",
		"Xcode-27",
	}

	for _, label := range testCases {
		t.Run(label, func(t *testing.T) {
			fixture := runnerWorkflow(t, label)
			selection := fixture.Views["test"].RunsOn().Selection()
			if got := PlatformOf(selection); got != MacOrLinux {
				t.Fatalf("platform for runner label %q is %v, want %v", label, got, MacOrLinux)
			}
		})
	}
}
