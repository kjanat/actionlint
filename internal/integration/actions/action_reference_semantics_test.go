package actions_test

import (
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actioncheck "actionlint.kjanat.dev/v2/internal/action/validation"
	"actionlint.kjanat.dev/v2/internal/testutil"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/yaml"
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
			check := func(errs []diagmodel.Diagnostic) {
				if invalid {
					if len(errs) != 1 || !strings.Contains(errs[0].Message, "do not support an '@ref' suffix") {
						t.Fatal(errs)
					}
				} else if len(errs) != 0 {
					t.Fatal(errs)
				}
			}
			check(testutil.WorkflowDiagnostics(t, workflow, ""))
			var meta actionmeta.Metadata
			source := `name: test
description: test
runs:
  using: composite
  steps:
    - uses: ` + spec + "\n"
			if err := yaml.Unmarshal([]byte(source), &meta); err != nil {
				t.Fatal(err)
			}
			findings := actioncheck.Check(&meta, &workflowlang.Pos{Line: 1, Col: 1})
			var diagnostics []diagmodel.Diagnostic
			for _, finding := range findings {
				diagnostics = append(diagnostics, finding.Diagnostic)
			}
			check(diagnostics)
		})
	}
}
