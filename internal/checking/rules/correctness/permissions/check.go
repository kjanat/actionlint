// Package permission validates explicit permission declarations.
package permission

import (
	"fmt"
	"maps"
	"slices"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	permissionmodel "actionlint.kjanat.dev/v2/internal/model/workflows/permissions"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/messages"
)

type Value struct {
	Text  string
	Start sourcepos.Position
}
type (
	Scope       struct{ Name, Level Value }
	Declaration struct {
		All    *Value
		Scopes []Scope
	}
)

func Check(input Declaration) []results.Diagnostic {
	scopes := permissionmodel.Scopes()
	var findings []results.Diagnostic
	report := func(start sourcepos.Position, message string) {
		findings = append(findings, results.Diagnostic{Rule: "permissions", Start: start, Message: message})
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
			report(scope.Name.Start, fmt.Sprintf("unknown permission scope %q. all available permission scopes are %s", scope.Name.Text, messages.SortedQuotes(slices.Collect(maps.Keys(scopes)))))
			continue
		}
		if !slices.Contains(allowed, scope.Level.Text) {
			report(scope.Level.Start, fmt.Sprintf("%q is invalid as permission of scope %q. available values are %s", scope.Level.Text, scope.Name.Text, messages.Quotes(allowed)))
		}
	}
	return findings
}
