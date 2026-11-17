package actions_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness/actioncheck"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestSelfRepositoryReferenceSuffix(t *testing.T) {
	t.Parallel()
	for _, spec := range []string{"$/foo@v1", "$/@v1", "$//@v1", "$/foo@", "$/foo@bar/baz", "$/", "$//", "$///", "$///foo", "./foo@v1"} {
		t.Run(spec, func(t *testing.T) {
			t.Parallel()
			invalid := strings.HasPrefix(spec, "$/") && strings.ContainsRune(spec, '@')
			workflow := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: ` + spec + "\n" + "\npermissions: {}\n"
			check := func(errs []results.Diagnostic) {
				if invalid {
					if len(errs) != 1 || !strings.Contains(errs[0].Message, "do not support an '@ref' suffix") {
						t.Fatal(errs)
					}
				} else if len(errs) != 0 {
					t.Fatal(errs)
				}
			}
			check(testutil.WorkflowDiagnostics(t, workflow, ""))
			source := `name: test
description: test
runs:
  using: composite
  steps:
    - uses: ` + spec + "\n"
			meta, err := actionmeta.ParseActionMetadata("action.yml", []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			findings := actioncheck.Check(meta, &sourcetext.Position{Line: 1, Column: 1}, actionmeta.ReferencedFiles{})
			var diagnostics []results.Diagnostic
			for _, finding := range findings {
				diagnostics = append(diagnostics, finding.Diagnostic)
			}
			check(diagnostics)
		})
	}
}
