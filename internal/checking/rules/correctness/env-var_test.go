package correctness

import (
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	mapping "actionlint.kjanat.dev/v2/internal/model/workflows/mappings"
)

func TestEnvironmentNameDiagnostics(t *testing.T) {
	t.Parallel()
	position := func(line int) sourcepos.Position { return sourcepos.Position{Line: line, Column: 7} }
	findings := environmentNameDiagnostics([]workflowfacts.Entry{
		{Name: "GOOD_NAME", State: mapping.KnownKey, Start: position(1)},
		{Name: "BAD NAME", State: mapping.KnownKey, Start: position(2)},
		{Name: "A=B", State: mapping.KnownKey, Start: position(3)},
		{Name: "A&B", State: mapping.KnownKey, Start: position(4)},
		{Name: "TAB\tNAME", State: mapping.KnownKey, Start: position(5)},
		{Name: "SKIPPED NAME", State: mapping.UnknownKey, Start: position(6)},
		{Name: "", State: mapping.InvalidKey, Start: position(7)},
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
		if finding.Start != position(want.line) || finding.Message != message {
			t.Fatalf("finding %d: %+v", i, finding)
		}
	}
	if findings := environmentNameDiagnostics(nil); findings != nil {
		t.Fatal(findings)
	}
}
