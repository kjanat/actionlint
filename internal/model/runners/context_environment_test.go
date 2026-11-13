package runner

import "testing"

func TestContextEnvironmentAvailability(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		reference, name     string
		workflow, composite bool
	}{
		{"github.action_path", "GITHUB_ACTION_PATH", false, true},
		{"GitHub.WORKSPACE", "GITHUB_WORKSPACE", true, true},
		{"runner.temp", "RUNNER_TEMP", true, true},
		{"github.token", "", false, false},
		{"github.event.issue.title", "", false, false},
		{"github.ref_protected", "", false, false},
		{"runner.workspace", "", false, false},
		{"", "", false, false},
	} {
		binding := ContextEnvironmentFor(tc.reference)
		if binding.Name() != tc.name || binding.Available(false) != tc.workflow || binding.Available(true) != tc.composite {
			t.Errorf("%s: incorrect environment binding %+v", tc.reference, binding)
		}
	}
}
