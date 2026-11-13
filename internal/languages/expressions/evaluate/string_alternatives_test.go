package evaluate

import (
	"slices"
	"strings"
	"testing"
)

func TestTemplateStringAlternatives(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, source string
		want         []string
	}{
		{"fmt command", "--allow-no-files --diff ${{ case(inputs.autofix == 'true', '--excludes .github', '') }}", []string{"--allow-no-files --diff --excludes .github", "--allow-no-files --diff "}},
		{"known true predicate", "${{ case(true, '--safe', inputs.unknown) }}", []string{"--safe"}},
		{"known false predicate", "${{ case(false, inputs.unknown, '--safe') }}", []string{"--safe"}},
		{"unknown output", "${{ case(inputs.autofix == 'true', inputs.version, '') }}", nil},
		{"invalid predicate", "${{ case('true', '--safe', '') }}", nil},
		{"duplicate outputs", "${{ case(inputs.enabled, 'a', 'a') }}", []string{"a"}},
		{"nested branch", "${{ case(inputs.a, case(inputs.b, 'a', 'b'), 'c') }}", []string{"a", "b", "c"}},
		{"multiple expressions", "${{ case(inputs.a, 'a', 'b') }}-${{ case(inputs.b, 'c', 'd') }}", []string{"a-c", "a-d", "b-c", "b-d"}},
		{"evaluated delimiters remain data", "${{ '${{ inputs.unknown }}' }}", []string{"${{ inputs.unknown }}"}},
		{"constant format", "${{ format('--option {0}', 'value') }}", []string{"--option value"}},
		{"trailing literal", "${{ case(inputs.a, 'a', 'b') }} suffix", []string{"a suffix", "b suffix"}},
		{"unterminated", "${{ case(inputs.a, 'a', 'b')", nil},
		{"budget", strings.Repeat("${{ case(inputs.a, 'a', 'b') }}", 6), nil},
		{"size budget", strings.Repeat("a", maxAlternativeBytes+1), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, complete := TemplateStringAlternatives(tc.source)
			if complete != (tc.want != nil) || !slices.Equal(got, tc.want) {
				t.Fatalf("alternatives=%v, complete=%t; want %v", got, complete, tc.want)
			}
		})
	}
}
