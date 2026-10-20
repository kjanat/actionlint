package action

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml"
)

func TestActionMetadataInsertionDuplicates(t *testing.T) {
	for _, section := range []string{"env", "with"} {
		for _, directive := range []string{"${{ insert }}", "${{insert}}"} {
			for _, tc := range []struct {
				name, mapping string
				duplicates    int
			}{
				{"direct", `{FOO: one, FOO: two}`, 1},
				{"before", `{FOO: one, INSERT: {FOO: two}}`, 1},
				{"after", `{INSERT: {FOO: one}, FOO: two}`, 1},
				{"nested", `{FOO: one, INSERT: {INSERT: {FOO: two}}}`, 1},
				{"literal key", `{FOO: one, INSERT: {"${{ 'FOO' }}": two}}`, 1},
				{"expression object", `{FOO: one, INSERT: "${{ fromJSON('{\"FOO\":\"two\"}') }}"}`, 1},
				{"alias", `{INSERT: &values {FOO: one}, FOO: two, INSERT: *values}`, 2},
				{"distinct", `{FOO: one, INSERT: {BAR: two}}`, 0},
				{"unknown", `{FOO: one, INSERT: '${{ inputs.extra }}'}`, 0},
			} {
				t.Run(section+"/"+directive+"/"+tc.name, func(t *testing.T) {
					mapping := strings.ReplaceAll(tc.mapping, "INSERT", "'"+directive+"'")
					source := "name: test\ndescription: test\ninputs:\n  extra:\n    description: extra\nruns:\n  using: composite\n  steps:\n    - uses: example/action@v1\n      " + section + ": " + mapping + "\n    - uses: example/action@v1\n      " + section + ": {FOO: separate}\n"
					var meta Metadata
					if err := yaml.Unmarshal([]byte(source), &meta); err != nil {
						t.Fatal(err)
					}
					errs := Validate(&meta)
					if len(errs) != tc.duplicates {
						t.Fatalf("want %d duplicates, got %v", tc.duplicates, errs)
					}
					for _, err := range errs {
						if !strings.Contains(err.Message, `duplicate key "FOO"`) || !strings.Contains(err.Message, "runs.steps[0]."+section) || err.Start.Line != 10 {
							t.Fatalf("unexpected duplicate location or message: %v", err)
						}
					}
				})
			}
		}
	}
}
