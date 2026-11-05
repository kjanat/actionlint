// Package envname validates resolved environment names without traversal state.
package envname

import (
	"fmt"
	"strings"

	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/results"
)

func Check(entries []workflowfacts.Entry) []results.Diagnostic {
	var findings []results.Diagnostic
	for _, entry := range entries {
		if entry.State == workflowfacts.UnknownKey || !strings.ContainsAny(entry.Name, "&= \t") {
			continue
		}
		findings = append(findings, results.Diagnostic{Rule: "env-var", Message: fmt.Sprintf("environment variable name %q is invalid. '&', '=' and spaces should not be contained", entry.Name), Start: entry.Start})
	}
	return findings
}
