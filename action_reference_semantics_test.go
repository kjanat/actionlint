package actionlint

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestSelfRepositoryReferenceSuffix(t *testing.T) {
	for _, spec := range []string{"$/foo@v1", "$/@v1", "$//@v1", "$/foo@", "$/foo@bar/baz", "$/", "$//", "$///", "$///foo", "./foo@v1"} {
		t.Run(spec, func(t *testing.T) {
			invalid := strings.HasPrefix(spec, "$/") && strings.ContainsRune(spec, '@')
			workflow := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: ` + spec + "\n"
			check := func(errs []*Error) {
				if invalid {
					if len(errs) != 1 || !strings.Contains(errs[0].Message, "do not support an '@ref' suffix") {
						t.Fatal(errs)
					}
				} else if len(errs) != 0 {
					t.Fatal(errs)
				}
			}
			check(lintCachePolicy(t, workflow, ""))
			var meta ActionMetadata
			source := `name: test
description: test
runs:
  using: composite
  steps:
    - uses: ` + spec + "\n"
			if err := yaml.Unmarshal([]byte(source), &meta); err != nil {
				t.Fatal(err)
			}
			rule := NewRuleAction(nil)
			rule.checkLocalActionMetadata(&meta, &ExecAction{Uses: &String{Value: "./test", Pos: &Pos{Line: 1, Col: 1}}})
			check(rule.Errs())
		})
	}
}

func TestCompositeOutputCaseCollisions(t *testing.T) {
	for _, second := range []string{"APPROVED", "denied"} {
		var meta ActionMetadata
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
		err := yaml.Unmarshal([]byte(source), &meta)
		if second == "APPROVED" {
			if err == nil || !strings.Contains(err.Error(), `output "APPROVED" is duplicated`) {
				t.Fatal(err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
}
