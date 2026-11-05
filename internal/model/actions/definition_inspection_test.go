package action

import (
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/results"
)

func TestInspectDefinitionRejectsInvalidManifest(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		line   int
		column int
	}{
		{"anchor", "name: &label example\ndescription: sample\nruns: {using: composite, steps: []}\n", 1, 7},
		{"alias", "name: example\ndescription: *missing\nruns: {using: composite, steps: []}\n", 2, 14},
		{"malformed YAML", "name: example\ndescription: [\n", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inspection := InspectDefinition([]byte(tc.source))
			if _, ok := inspection.Definition(); ok {
				t.Fatal("invalid manifest produced a definition")
			}
			diagnostics := inspection.Diagnostics()
			if len(diagnostics) == 0 {
				t.Fatal("invalid manifest produced no diagnostics")
			}
			if diagnostics[0].Rule != "action" || diagnostics[0].Path != "action.yml" {
				t.Fatalf("diagnostic lost manifest identity: %+v", diagnostics[0])
			}
			if tc.line != 0 && diagnostics[0].Start != (results.Position{Line: tc.line, Column: tc.column}) {
				t.Fatalf("diagnostic position = %+v, want %d:%d", diagnostics[0].Start, tc.line, tc.column)
			}
			// Returned diagnostic storage must not alias the inspection result.
			diagnostics[0].Message = "changed"
			if strings.Contains(inspection.Diagnostics()[0].Message, "changed") {
				t.Fatal("diagnostics exposed mutable internal storage")
			}
		})
	}
}

func TestInspectDefinitionReturnsValidatedImmutableFacts(t *testing.T) {
	source := []byte("name: example\ndescription: 'literal &label *label'\nruns: {using: composite, steps: []}\n# &comment *comment\ninputs:\n  token:\n    description: token\n")
	inspection := InspectDefinition(source)
	definition, ok := inspection.Definition()
	if !ok {
		t.Fatalf("valid manifest diagnostics: %+v", inspection.Diagnostics())
	}
	if !definition.Present() || definition.Using() != "composite" {
		t.Fatalf("unexpected definition facts: present=%v using=%q", definition.Present(), definition.Using())
	}
	first := definition.InputNames()
	first[0] = "changed"
	second := definition.InputNames()
	if !slices.Equal(second, []string{"token"}) {
		t.Fatalf("input names exposed mutable internal storage: %v", second)
	}
	if len(inspection.Diagnostics()) != 0 {
		t.Fatalf("valid manifest diagnostics: %+v", inspection.Diagnostics())
	}
}

func TestInspectDefinitionRejectsEmptyManifest(t *testing.T) {
	inspection := InspectDefinition(nil)
	if _, ok := inspection.Definition(); ok {
		t.Fatal("empty manifest produced a definition")
	}
	if len(inspection.Diagnostics()) == 0 {
		t.Fatal("empty manifest produced no diagnostic")
	}
}

func TestInspectDefinitionOutputKnowledge(t *testing.T) {
	for _, test := range []struct {
		name, declaration string
		unknown           bool
		names             []string
	}{
		{name: "omitted outputs", unknown: true},
		{name: "explicit empty outputs", declaration: "outputs: {}\n"},
		{name: "declared output", declaration: "outputs: {result: {description: result, value: ready}}\n", names: []string{"result"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			inspection := InspectDefinition([]byte("name: fixture\ndescription: fixture\n" + test.declaration + "runs: {using: composite, steps: []}\n"))
			definition, valid := inspection.Definition()
			if !valid {
				t.Fatal(inspection.Diagnostics())
			}
			if definition.SkipOutputs() != test.unknown || !slices.Equal(definition.OutputNames(), test.names) {
				t.Fatalf("output knowledge: unknown=%v names=%v", definition.SkipOutputs(), definition.OutputNames())
			}
		})
	}
}
