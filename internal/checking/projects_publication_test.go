package checking

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestCombinedProjectsPreserveIncompleteChecks(t *testing.T) {
	t.Parallel()
	checks := []report.IncompleteCheck{
		{Source: sourcetext.Reference{Document: "first/ci.yml", Revision: "first"}, Rule: "shellcheck", Reason: "tool unavailable"},
		{Source: sourcetext.Reference{Document: "second/ci.yml", Revision: "second"}, Reason: "policy evaluation failed"},
	}
	var combined report.Analysis
	for _, check := range checks {
		var err error
		combined, err = report.Merge(combined, report.Analysis{IncompleteChecks: []report.IncompleteCheck{check}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(combined.IncompleteChecks, checks) {
		t.Fatalf("combined repository checks lost completion evidence:\n got: %+v\nwant: %+v", combined.IncompleteChecks, checks)
	}
}
