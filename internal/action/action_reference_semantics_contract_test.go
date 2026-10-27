package action_test

import (
	"strings"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
)

func TestCompositeOutputCaseCollisions(t *testing.T) {
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
