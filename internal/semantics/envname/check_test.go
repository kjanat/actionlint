package envname

import (
	"testing"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
)

func TestCheck(t *testing.T) {
	position := func(line int) diagnostic.Position { return diagnostic.Position{Line: line, Column: 7} }
	findings := Check([]analyzer.Entry{
		{Name: "GOOD_NAME", State: analyzer.KnownKey, Start: position(1)},
		{Name: "BAD NAME", State: analyzer.KnownKey, Start: position(2)},
		{Name: "A=B", State: analyzer.KnownKey, Start: position(3)},
		{Name: "A&B", State: analyzer.KnownKey, Start: position(4)},
		{Name: "TAB\tNAME", State: analyzer.KnownKey, Start: position(5)},
		{Name: "SKIPPED NAME", State: analyzer.UnknownKey, Start: position(6)},
		{Name: "", State: analyzer.InvalidKey, Start: position(7)},
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
