package value

import (
	"slices"
	"testing"
)

func TestStringAlternativesPreserveEvaluationBoundary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		value Value
		want  []string
	}{
		{"literal data", YAMLScalar("${{ inputs.value }}", "!!str", Origin{}), []string{"${{ inputs.value }}"}},
		{"evaluated delimiters", Template("${{ '${{ inputs.value }}' }}", Origin{}), []string{"${{ inputs.value }}"}},
		{"finite template", Template("--diff ${{ case(inputs.x == 'true', '--exclude .github', '') }}", Origin{}), []string{"--diff --exclude .github", "--diff "}},
		{"unknown template output", Template("${{ case(inputs.x == 'true', inputs.value, '') }}", Origin{}), nil},
		{"unresolved value", DynamicValue("safe", String, Origin{}), nil},
		{"missing", Value{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			values, complete := tc.value.StringAlternatives()
			if complete != (tc.want != nil) || !slices.Equal(values, tc.want) {
				t.Fatalf("alternatives=%v, complete=%t; want %v", values, complete, tc.want)
			}
			if len(values) > 0 {
				values[0] = "caller change"
				fresh, _ := tc.value.StringAlternatives()
				if !slices.Equal(fresh, tc.want) {
					t.Fatal("caller mutation changed the semantic value")
				}
			}
		})
	}
}
