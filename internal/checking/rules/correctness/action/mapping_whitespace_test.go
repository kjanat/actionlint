package action_test

import (
	"strings"
	"testing"

	actioncheck "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/action"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
)

func TestExpressionWhitespaceActionMappingBoundaries(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"env", "with"} {
		for _, tc := range []struct {
			name, value string
			invalid     bool
		}{
			{"quoted map", `" ${{ fromJSON('{}') }} "`, true},
			{"quoted unknown map", `" ${{ fromJSON(inputs.value) }} "`, true},
			{"block map", "|\n        ${{ fromJSON('{}') }}", true},
			{"plain map", "  ${{ fromJSON('{}') }}  ", false},
			{"stripped block map", "|-\n        ${{ fromJSON('{}') }}", false},
		} {
			t.Run(field+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				source := []byte(`name: test
description: test
runs:
  using: composite
  steps:
    - uses: actions/checkout@v6
      ` + field + ": " + tc.value + "\n")
				meta, err := actionmeta.ParseActionMetadata("action.yml", source)
				if err != nil {
					t.Fatal(err)
				}
				meta.Filename, meta.Source = "action.yml", source
				findings := actioncheck.Check(meta, &sourcepos.Position{Line: 1, Column: 1}, actionmeta.ReferencedFiles{})
				errs := findings
				if tc.invalid {
					if len(errs) != 1 || !strings.Contains(errs[0].Message, "expected a mapping") {
						t.Fatalf("want one mapping diagnostic, got %v", errs)
					}
				} else if len(errs) != 0 {
					t.Fatalf("valid mapping rejected: %v", errs)
				}
			})
		}
	}
}
