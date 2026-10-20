// Package permission validates explicit permission declarations.
package permission

import (
	"fmt"
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/stringutil"
)

type Value struct {
	Text  string
	Start diagnostic.Position
}
type Scope struct{ Name, Level Value }
type Declaration struct {
	All    *Value
	Scopes []Scope
}

func Check(input Declaration) []diagnostic.Diagnostic {
	var findings []diagnostic.Diagnostic
	report := func(start diagnostic.Position, message string) {
		findings = append(findings, diagnostic.Diagnostic{Rule: "permissions", Start: start, Message: message})
	}
	if all := input.All; all != nil {
		if all.Text != "write-all" && all.Text != "read-all" {
			report(all.Start, fmt.Sprintf("%q is invalid for permission for all the scopes. available values are \"read-all\", \"write-all\" or {}", all.Text))
		}
		return findings
	}
	for _, scope := range input.Scopes {
		allowed, ok := scopes[scope.Name.Text]
		if !ok {
			report(scope.Name.Start, fmt.Sprintf("unknown permission scope %q. all available permission scopes are %s", scope.Name.Text, stringutil.SortedQuotes(slices.Collect(maps.Keys(scopes)))))
			continue
		}
		if !slices.Contains(allowed, scope.Level.Text) {
			report(scope.Level.Start, fmt.Sprintf("%q is invalid as permission of scope %q. available values are %s", scope.Level.Text, scope.Name.Text, stringutil.Quotes(allowed)))
		}
	}
	return findings
}
