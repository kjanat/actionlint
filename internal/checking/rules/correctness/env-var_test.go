package correctness

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/contexts/value"
	"actionlint.kjanat.dev/v2/internal/model/workflows/mappings"
)

func TestEnvironmentNameDiagnostics(t *testing.T) {
	t.Parallel()
	position := func(line int) source.Position { return source.Position{Line: line, Column: 7} }
	origin := func(line int) value.Origin { return value.Origin{Span: value.Span{Start: position(line)}} }
	findings := environmentNameDiagnostics([]mappings.Declaration{
		{Name: "GOOD_NAME", State: mappings.KnownKey, Origin: origin(1)},
		{Name: "BAD NAME", State: mappings.KnownKey, Origin: origin(2)},
		{Name: "A=B", State: mappings.KnownKey, Origin: origin(3)},
		{Name: "A&B", State: mappings.KnownKey, Origin: origin(4)},
		{Name: "TAB\tNAME", State: mappings.KnownKey, Origin: origin(5)},
		{Name: "SKIPPED NAME", State: mappings.UnknownKey, Origin: origin(6)},
		{Name: "", State: mappings.InvalidKey, Origin: origin(7)},
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
