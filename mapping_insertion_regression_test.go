package actionlint

import (
	"fmt"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestUnknownMatrixKeysPreserveKnownTypes(t *testing.T) {
	for depth := range 3 {
		t.Run(fmt.Sprintf("depth=%d", depth), func(t *testing.T) {
			matrix := `{'${{ vars.AXIS }}': [ubuntu-latest], count: [1]}`
			for range depth {
				matrix = "{'${{ insert }}': " + matrix + "}"
			}
			source := "on: push\njobs:\n  test:\n    runs-on: ${{ matrix.os }}\n    strategy:\n      matrix: " + matrix + "\n    steps: [{run: echo ok}]\n"
			if errs := lintCredentialsExpressionTest(t, source); len(errs) != 0 {
				t.Fatalf("unknown axis rejected: %v", errs)
			}
			workflow, errs := Parse([]byte(source))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			rule := NewRuleExpression(nil, nil)
			ty := rule.checkMatrix(workflow.Jobs["test"].Strategy.Matrix)
			if !ty.IsLoose() || len(ty.Props) != 1 {
				t.Fatalf("want open object with only the known axis, got %#v", ty)
			}
			if count, ok := ty.Props["count"]; !ok || count.String() != "number" {
				t.Fatalf("known axis type lost: %#v", ty.Props)
			}
		})
	}
}

func TestKnownMatrixKeysStayStrict(t *testing.T) {
	for _, key := range []string{"os", "${{ 'os' }}", "${{ format('{0}', 'os') }}"} {
		t.Run(key, func(t *testing.T) {
			source := insertionSectionWorkflow("matrix", fmt.Sprintf("{'${{ insert }}': {%q: [ubuntu-latest]}}", key))
			workflow, errs := Parse([]byte(source))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			rule := NewRuleExpression(nil, nil)
			for _, job := range workflow.Jobs {
				ty := rule.checkMatrix(job.Strategy.Matrix)
				if !ty.IsStrict() || len(ty.Props) != 1 || ty.Props["os"] == nil {
					t.Fatalf("known key lost or object loosened: %#v", ty)
				}
			}
		})
	}
	// Preserve expression delimiters in evaluated keys as literal data.
	const key = "${{ vars.AXIS }}"
	matrix := knownLiteralMatrix(map[string]any{key: []any{"ubuntu-latest"}}, &Pos{Line: 1, Col: 1})
	if matrix == nil {
		t.Fatal("literal matrix rejected")
	}
	rule := NewRuleExpression(nil, nil)
	ty := rule.checkMatrix(matrix)
	if !ty.IsStrict() || len(ty.Props) != 1 || ty.Props[strings.ToLower(key)] == nil || len(rule.Errs()) != 0 {
		t.Fatalf("evaluated key reinterpreted: %#v, %v", ty, rule.Errs())
	}
}

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
					var meta ActionMetadata
					if err := yaml.Unmarshal([]byte(source), &meta); err != nil {
						t.Fatal(err)
					}
					rule := NewRuleAction(nil)
					rule.checkActionMetadataSchema(&meta)
					if errs := rule.Errs(); len(errs) != tc.duplicates {
						t.Fatalf("want %d duplicates, got %v", tc.duplicates, errs)
					}
					for _, err := range rule.Errs() {
						if !strings.Contains(err.Message, `duplicate key "FOO"`) || !strings.Contains(err.Message, "runs.steps[0]."+section) || err.Line != 10 {
							t.Fatalf("unexpected duplicate location or message: %v", err)
						}
					}
				})
			}
		}
	}
}
