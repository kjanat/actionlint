package actionmeta_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
)

func TestCompositeOutputCaseCollisions(t *testing.T) {
	t.Parallel()
	for _, second := range []string{"APPROVED", "denied"} {
		source := `name: test
description: test
outputs:
  approved: {value: 'false'}
  ` + second + `: {value: 'true'}
runs:
  using: composite
  steps:
    - run: echo ok
      shell: bash
`
		_, err := actionmeta.Parse("action.yml", []byte(source))
		if second == "APPROVED" {
			if err == nil || !strings.Contains(err.Error(), `output "APPROVED" is duplicated`) {
				t.Fatal(err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
}
