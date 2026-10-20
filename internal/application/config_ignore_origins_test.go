package application

import (
	"strings"
	"testing"
)

func TestReusableIgnoresDistinguishAliasedSteps(t *testing.T) {
	const source = `on: push
jobs:
  a:
    runs-on: ubuntu-latest
    steps: &shared
      - id: validate
        run: echo '${{ invalid }}'
  b:
    runs-on: ubuntu-latest
    steps: *shared
`
	for _, job := range []string{"a", "b"} {
		for _, source := range []string{source, strings.NewReplacer("steps: &shared\n      - id:", "steps:\n      - &shared\n        id:", "steps: *shared", "steps:\n      - *shared").Replace(source)} {
			findings := analyzeConfiguredWorkflow(t, source, "ignores: [{rule: expression-reference, job: "+job+", step: validate}]")
			if len(findings) != 1 || findings[0].Rule != "expression-reference" || findings[0].IgnoreOrigin == nil || findings[0].IgnoreOrigin.Job == job {
				t.Fatalf("ignore job %s did not retain other invocation: %+v", job, findings)
			}
		}
	}
}

func TestReusableIgnoresDistinguishAliasedActionSteps(t *testing.T) {
	const source = `on: push
jobs:
  a:
    runs-on: ubuntu-latest
    steps: &shared
      - id: validate
        uses: invalid
  b:
    runs-on: ubuntu-latest
    steps: *shared
`
	findings := analyzeConfiguredWorkflow(t, source, "ignores: [{rule: action, job: a, uses: invalid}]")
	if len(findings) != 1 || findings[0].Rule != "action" || findings[0].IgnoreOrigin == nil || findings[0].IgnoreOrigin.Job != "b" {
		t.Fatalf("lost alias invocation: %+v", findings)
	}
}
