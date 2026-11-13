package action

import (
	"slices"
	"strings"
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
)

func TestInspectDefinitionRejectsInvalidManifest(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			inspection := InspectDefinition([]byte(tc.source))
			if _, ok := inspection.Definition(); ok {
				t.Fatal("invalid manifest produced a definition")
			}
			problems := inspection.Problems()
			if len(problems) == 0 {
				t.Fatal("invalid manifest produced no problems")
			}
			if problems[0].Code == "" || problems[0].Path != "action.yml" {
				t.Fatalf("problem lost manifest identity: %+v", problems[0])
			}
			if tc.line != 0 && problems[0].Start != (sourcepos.Position{Line: tc.line, Column: tc.column}) {
				t.Fatalf("problem position = %+v, want %d:%d", problems[0].Start, tc.line, tc.column)
			}
			// Returned problem storage must not alias the inspection result.
			problems[0].Message = "changed"
			if strings.Contains(inspection.Problems()[0].Message, "changed") {
				t.Fatal("problems exposed mutable internal storage")
			}
		})
	}
}

func TestInspectDefinitionReturnsValidatedImmutableFacts(t *testing.T) {
	t.Parallel()
	source := []byte("name: example\ndescription: 'literal &label *label'\nruns: {using: composite, steps: []}\n# &comment *comment\ninputs:\n  token:\n    description: token\n")
	inspection := InspectDefinition(source)
	definition, ok := inspection.Definition()
	if !ok {
		t.Fatalf("valid manifest problems: %+v", inspection.Problems())
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
	if len(inspection.Problems()) != 0 {
		t.Fatalf("valid manifest problems: %+v", inspection.Problems())
	}
}

func TestInspectDefinitionRejectsEmptyManifest(t *testing.T) {
	t.Parallel()
	inspection := InspectDefinition(nil)
	if _, ok := inspection.Definition(); ok {
		t.Fatal("empty manifest produced a definition")
	}
	if len(inspection.Problems()) == 0 {
		t.Fatal("empty manifest produced no diagnostic")
	}
}

func TestInspectDefinitionOutputKnowledge(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, declaration, runs string
		complete                bool
		names                   []string
	}{
		{"composite omitted", "", "{using: composite, steps: []}", true, nil},
		{"composite empty", "outputs: {}\n", "{using: composite, steps: []}", true, nil},
		{"composite declared", "outputs: {result: {description: result, value: ready}}\n", "{using: composite, steps: []}", true, []string{"result"}},
		{"node omitted", "", "{using: node24, main: index.js}", false, nil},
		{"node empty", "outputs: {}\n", "{using: node24, main: index.js}", false, nil},
		{"node declared", "outputs: {result: {description: result}}\n", "{using: node24, main: index.js}", false, []string{"result"}},
		{"docker declared", "outputs: {result: {description: result}}\n", "{using: docker, image: Dockerfile}", false, []string{"result"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			inspection := InspectDefinition([]byte("name: fixture\ndescription: fixture\n" + test.declaration + "runs: " + test.runs + "\n"))
			definition, valid := inspection.Definition()
			if !valid {
				t.Fatal(inspection.Problems())
			}
			if definition.OutputsComplete() != test.complete || !slices.Equal(definition.OutputNames(), test.names) {
				t.Fatalf("output knowledge: complete=%v names=%v", definition.OutputsComplete(), definition.OutputNames())
			}
		})
	}
	for _, runs := range []string{"{}", "{using: unknown}", "{using: composite}"} {
		definition := ParseDefinition([]byte("name: fixture\ndescription: fixture\noutputs: {}\nruns: " + runs + "\n"))
		if definition.OutputsComplete() {
			t.Fatalf("invalid runtime %s established complete outputs", runs)
		}
	}
	if NewDefinition(&Metadata{Runs: Runs{Using: "composite"}}).OutputsComplete() {
		t.Fatal("unvalidated metadata established complete outputs")
	}
}
