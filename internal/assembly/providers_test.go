package assembly

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/input/configuration/configreport"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func toolRegistrations(names ...string) []contract.Registration {
	registrations := make([]contract.Registration, len(names))
	for i, name := range names {
		registrations[i].Name = name
	}
	return registrations
}

// A tool's status follows the documents it completed: a path override can
// enable it where the base configuration disables it, and one incomplete
// check does not hide the work it finished elsewhere.
func TestProviderStatusesFollowParticipation(t *testing.T) {
	t.Parallel()
	disabled := map[string]any{"tools": map[string]any{
		"ruff": map[string]any{"enabled": false}, "zizmor": map[string]any{"enabled": false},
	}}
	result := &report.Analysis{
		Configurations: []configreport.Report{{Inspection: configreport.Inspection{Config: disabled}}},
		CollectedByRule: []report.RuleCollection{
			{Rule: "ruff", Findings: 2, Completed: 1},
			{Rule: "shellcheck", Completed: 3},
			{Rule: "actionlint-tool", Completed: 0},
			{Rule: "zizmor", Completed: 0},
		},
		IncompleteChecks: []report.IncompleteCheck{
			{Rule: "shellcheck", Reason: "timed out"},
			{Rule: "shellcheck", Reason: "exited with status 3"},
			{Rule: "shellcheck", Reason: "timed out"},
			{Rule: "actionlint-tool", Reason: "executable not found"},
		},
	}
	got := providerStatuses(pluginselection.Sources{}, toolRegistrations("ruff", "shellcheck", "actionlint-tool", "zizmor"), result)
	want := []report.ProviderStatus{
		{Kind: report.ToolProvider, Name: "actionlint-tool", Inactive: inactiveNoCheck, Incomplete: []string{"executable not found"}},
		{Kind: report.ToolProvider, Name: "ruff"},
		{Kind: report.ToolProvider, Name: "shellcheck", Incomplete: []string{"exited with status 3", "timed out"}},
		{Kind: report.ToolProvider, Name: "zizmor", Inactive: inactiveDisabledConfig},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("statuses = %+v, want %+v", got, want)
	}
}
