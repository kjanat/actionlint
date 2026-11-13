package runner

import (
	"testing"

	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
)

func TestContainerTargetExcludesOnlyProvedFalseConditions(t *testing.T) {
	t.Parallel()
	target := ContainerTarget{OS: runnerapi.Windows}
	for _, tc := range []struct {
		name   string
		guards []string
		want   bool
	}{
		{"no guard", nil, true},
		{"wrong OS", []string{"runner.os == 'Linux'"}, false},
		{"matching OS", []string{"runner.os == 'Windows'"}, true},
		{"unknown context", []string{"inputs.run"}, true},
		{"unknown and false", []string{"inputs.run && false"}, false},
		{"invalid expression", []string{"contains("}, true},
		{"enclosing skipped call", []string{"always()", "false"}, false},
		{"status and OS", []string{"success() && runner.os == 'Linux'"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := target.MayRun(tc.guards...); got != tc.want {
				t.Fatalf("MayRun(%v)=%t, want %t", tc.guards, got, tc.want)
			}
		})
	}
}
