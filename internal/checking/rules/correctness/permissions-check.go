package correctness

import (
	"fmt"
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	permissionmodel "actionlint.kjanat.dev/v2/internal/model/workflows/permissions"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/messages"
)

func checkPermissionMapping(p *workflowlang.Permissions) []reporting.Finding {
	if p == nil {
		return nil
	}
	input := permissionDeclaration{}
	if p.All != nil {
		input.All = &permissionValue{Text: p.All.Value, Start: sourcepos.ZeroIfNil(p.All.Pos)}
	}
	for _, scope := range p.Scopes {
		input.Scopes = append(input.Scopes, permissionScope{Name: permissionValue{Text: scope.Name.Text(), Start: sourcepos.ZeroIfNil(reporting.SourcePosition(scope.Name))}, Level: permissionValue{Text: scope.Value.Text(), Start: sourcepos.ZeroIfNil(reporting.SourcePosition(scope.Value))}})
	}
	return reporting.FromDiagnostics(permissionDiagnostics(input))
}

func checkWorkflowPermissions(w declaration.Workflow) []reporting.Finding {
	return checkPermissionMapping(w.Permissions)
}

func checkJobPermissions(job declaration.Job) []reporting.Finding {
	return checkPermissionMapping(job.Permissions)
}

type permissionValue struct {
	Text  string
	Start sourcepos.Position
}
type (
	permissionScope       struct{ Name, Level permissionValue }
	permissionDeclaration struct {
		All    *permissionValue
		Scopes []permissionScope
	}
)

// permissionDiagnostics validates explicit permission declarations against the known scopes and levels.
func permissionDiagnostics(input permissionDeclaration) []results.Diagnostic {
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
