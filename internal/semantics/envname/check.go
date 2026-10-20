// Package envname validates resolved environment names without traversal state.
package envname

import (
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
)

func Check(entries []analyzer.Entry) []diagnostic.Diagnostic {
	var findings []diagnostic.Diagnostic
	for _, entry := range entries {
		if entry.State == analyzer.UnknownKey || !strings.ContainsAny(entry.Name, "&= \t") {
			continue
		}
		findings = append(findings, diagnostic.Diagnostic{Rule: "env-var", Message: fmt.Sprintf("environment variable name %q is invalid. '&', '=' and spaces should not be contained", entry.Name), Start: entry.Start})
	}
	return findings
}
