package envname

import (
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
)

func TestCheck(t *testing.T) {
	t.Parallel()
	position := func(line int) sourcepos.Position { return sourcepos.Position{Line: line, Column: 7} }
	findings := Check([]workflowfacts.Entry{
		{Name: "GOOD_NAME", State: workflowfacts.KnownKey, Start: position(1)},
		{Name: "BAD NAME", State: workflowfacts.KnownKey, Start: position(2)},
		{Name: "A=B", State: workflowfacts.KnownKey, Start: position(3)},
		{Name: "A&B", State: workflowfacts.KnownKey, Start: position(4)},
		{Name: "TAB\tNAME", State: workflowfacts.KnownKey, Start: position(5)},
		{Name: "SKIPPED NAME", State: workflowfacts.UnknownKey, Start: position(6)},
		{Name: "", State: workflowfacts.InvalidKey, Start: position(7)},
	})
	if len(findings) != 4 {
		t.Fatalf("findings: %+v", findings)
	}
	for i, want := range []struct {
		line int
		name string
	}{{2, "BAD NAME"}, {3, "A=B"}, {4, "A&B"}, {5, "TAB\tNAME"}} {
		finding := findings[i]
		message := "environment variable name \"" + want.name + "\" is invalid. '&', '=' and spaces should not be contained"
		if want.name == "TAB\tNAME" {
			message = `environment variable name "TAB\tNAME" is invalid. '&', '=' and spaces should not be contained`
		}
		if finding.Rule != "env-var" || finding.Start != position(want.line) || finding.Message != message {
			t.Fatalf("finding %d: %+v", i, finding)
		}
	}
	if findings := Check(nil); findings != nil {
		t.Fatal(findings)
	}
}
