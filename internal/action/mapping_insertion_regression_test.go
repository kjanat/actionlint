package action

import (
	"strings"
	"testing"
)

func TestActionMetadataInsertionDuplicates(t *testing.T) {
	for _, section := range []string{"env", "with"} {
		for _, directive := range []string{"${{ insert }}", "${{insert}}"} {
			for _, tc := range []struct {
				name, mapping           string
				duplicates, unsupported int
			}{
				{"direct", `{FOO: one, FOO: two}`, 1, 0},
				{"before", `{FOO: one, INSERT: {FOO: two}}`, 1, 0},
				{"after", `{INSERT: {FOO: one}, FOO: two}`, 1, 0},
				{"nested", `{FOO: one, INSERT: {INSERT: {FOO: two}}}`, 1, 0},
				{"literal key", `{FOO: one, INSERT: {"${{ 'FOO' }}": two}}`, 1, 0},
				{"expression object", `{FOO: one, INSERT: "${{ fromJSON('{\"FOO\":\"two\"}') }}"}`, 1, 0},
				{"alias", `{INSERT: &values {FOO: one}, FOO: two, INSERT: *values}`, 2, 2},
				{"distinct", `{FOO: one, INSERT: {BAR: two}}`, 0, 0},
				{"unknown", `{FOO: one, INSERT: '${{ inputs.extra }}'}`, 0, 0},
			} {
				t.Run(section+"/"+directive+"/"+tc.name, func(t *testing.T) {
					mapping := strings.ReplaceAll(tc.mapping, "INSERT", "'"+directive+"'")
					source := "name: test\ndescription: test\ninputs:\n  extra:\n    description: extra\nruns:\n  using: composite\n  steps:\n    - uses: example/action@v1\n      " + section + ": " + mapping + "\n    - uses: example/action@v1\n      " + section + ": {FOO: separate}\n"
					meta, err := Parse("action.yml", []byte(source))
					if err != nil {
						t.Fatal(err)
					}
					errs := Validate(meta)
					if len(errs) != tc.duplicates+tc.unsupported {
						t.Fatalf("want %d duplicates and %d unsupported-syntax errors, got %v", tc.duplicates, tc.unsupported, errs)
					}
					var duplicates, unsupported int
					for _, err := range errs {
						if strings.Contains(err.Message, "YAML anchors") || strings.Contains(err.Message, "YAML aliases") {
							unsupported++
							if err.Start.Line != 10 {
								t.Fatalf("unsupported alias lost source location: %v", err)
							}
							continue
						}
						duplicates++
						if !strings.Contains(err.Message, `duplicate key "FOO"`) || !strings.Contains(err.Message, "runs.steps[0]."+section) || err.Start.Line != 10 {
							t.Fatalf("unexpected duplicate location or message: %v", err)
						}
					}
					if duplicates != tc.duplicates || unsupported != tc.unsupported {
						t.Fatalf("got %d duplicates and %d unsupported-syntax errors, want %d and %d", duplicates, unsupported, tc.duplicates, tc.unsupported)
					}
				})
			}
		}
	}
}
