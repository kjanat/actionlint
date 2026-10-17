package engine

import (
	"strings"
	"testing"
)

func TestSuspiciousConditionChecks(t *testing.T) {
	for _, expression := range []string{
		"github.ref == 'refs/heads/main'", "github.actor != 'admin'",
		"startsWith(github.ref, 'refs/heads/')", "contains(github.event.label.name, 'approved')",
		"endsWith(github.event.deployment.environment, 'production')",
	} {
		for _, location := range []string{"job", "step", "snapshot"} {
			for _, config := range []string{"", "lint: {rules: {suspicious: off}}", "lint: {rules: {suspicious: {case-insensitive-conditions: on}}}"} {
				t.Run(expression+"/"+location+"/"+config, func(t *testing.T) {
					body := `on: [push, label, deployment]
jobs:
  test:
    runs-on: ubuntu-latest
`
					switch location {
					case "job":
						body += "    if: " + expression + `
    steps:
      - run: echo ok
`
					case "step":
						body += `    steps:
      - run: echo ok
        if: ` + expression + "\n"
					case "snapshot":
						body += `    snapshot:
      image-name: test
      if: ` + expression + `
    steps:
      - run: echo ok
`
					}
					errs := lintCachePolicy(t, body, config)
					if strings.Contains(config, ": on") {
						if len(errs) != 1 || errs[0].Rule != "case-insensitive-conditions" {
							t.Fatal(errs)
						}
					} else if len(errs) != 0 {
						t.Fatal(errs)
					}
				})
			}
		}
	}
}
