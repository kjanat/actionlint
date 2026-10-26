package action_test

import (
	"strings"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
)

func TestCompositeOutputCaseCollisions(t *testing.T) {
	for _, second := range []string{"APPROVED", "denied"} {
		var meta actionmeta.Metadata
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
		err := codec.Unmarshal([]byte(source), &meta)
		if second == "APPROVED" {
			if err == nil || !strings.Contains(err.Error(), `output "APPROVED" is duplicated`) {
				t.Fatal(err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
}
