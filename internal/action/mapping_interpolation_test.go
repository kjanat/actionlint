package action_test

import (
	"strings"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
)

func TestActionMappingInterpolationReview(t *testing.T) {
	for _, field := range []string{"env", "with"} {
		for _, value := range []string{"prefix-${{ inputs.value }}", "${{ inputs.value }}-suffix", "${{ inputs.value }}tail}}", "${{ inputs.value }}${{ inputs.value }}", "${{ fromJSON(inputs.value) }}", `${{ fromJSON('{"key":"${{"}') }}`} {
			t.Run(field+"/"+value, func(t *testing.T) {
				source := `name: test
description: test
inputs:
  value:
    description: test
runs:
  using: composite
  steps:
    - uses: actions/checkout@v6
      ` + field + ": " + value + "\n"
				meta, err := actionmeta.Parse("action.yml", []byte(source))
				if err != nil {
					t.Fatal(err)
				}
				findings := actionmeta.Validate(meta)
				want := 1
				if strings.HasPrefix(value, "${{ fromJSON(") {
					want = 0
				}
				if len(findings) != want || (want == 1 && !strings.Contains(findings[0].Message, "expected a mapping")) {
					t.Fatalf("want %d mapping errors, got %v", want, findings)
				}
			})
		}
	}
}
