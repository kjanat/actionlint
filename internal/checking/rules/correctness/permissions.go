package correctness

import (
	"fmt"
	"maps"
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/messages"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/model/workflows/permissions"
	"actionlint.kjanat.dev/v2/internal/results"
)

// PermissionsRule is the single definition of the permissions rule.
var PermissionsRule = &define.Rule{
	Name:        "permissions",
	Description: "Validate token permission scopes and access levels",
	Category:    "correctness",
	Subcategory: "permissions",
	Recommended: true,
	Help:        "Use supported permission scopes and read, write or none access levels.",
	Examples: []rule.Example{rule.WorkflowExample("Choose an allowed access level for the permission scope.",
		"    permissions:\n      contents: execute\n", "    permissions:\n      contents: read\n")},
	Checks: define.Fixed(define.Checks{Workflow: checkWorkflowPermissions, Job: checkJobPermissions}),
}

func checkPermissionMapping(p *definition.Permissions) []reporting.StepFinding {
	if p == nil {
		return nil
	}
	input := permissionDeclaration{}
	if p.All != nil {
		input.All = &permissionValue{Text: p.All.Value, Start: sourcetext.ZeroIfNil(p.All.Pos)}
	}
	for _, scope := range p.Scopes {
		input.Scopes = append(input.Scopes, permissionScope{Name: permissionValue{Text: scope.Name.Text(), Start: sourcetext.ZeroIfNil(reporting.SourcePosition(scope.Name))}, Level: permissionValue{Text: scope.Value.Text(), Start: sourcetext.ZeroIfNil(reporting.SourcePosition(scope.Value))}})
	}
	return reporting.FromDiagnostics(permissionDiagnostics(input))
}

func checkWorkflowPermissions(w workflows.BoundWorkflow) []reporting.StepFinding {
	return checkPermissionMapping(w.Permissions)
}

func checkJobPermissions(job workflows.BoundJob) []reporting.StepFinding {
	return checkPermissionMapping(job.Permissions)
}

type permissionValue struct {
	Text  string
	Start sourcetext.Position
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
	scopes := permissions.Scopes()
	var findings []results.Diagnostic
	report := func(start sourcetext.Position, message string) {
		findings = append(findings, results.Diagnostic{Start: start, Message: message})
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
