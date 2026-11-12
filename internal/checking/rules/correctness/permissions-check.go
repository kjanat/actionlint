package correctness

import (
	permission "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/permissions"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

func checkPermissionMapping(p declaration.PermissionsView) []reporting.Finding {
	if !p.Present() {
		return nil
	}
	input := permission.Declaration{}
	if p.All().Present() {
		input.All = &permission.Value{Text: p.All().Value(), Start: reporting.Position(reporting.ViewPosition(p.All().Pos()))}
	}
	for _, scope := range p.Scopes() {
		input.Scopes = append(input.Scopes, permission.Scope{Name: permission.Value{Text: scope.Name().Value(), Start: reporting.Position(reporting.ViewPosition(scope.Name().Pos()))}, Level: permission.Value{Text: scope.Value().Value(), Start: reporting.Position(reporting.ViewPosition(scope.Value().Pos()))}})
	}
	return reporting.FromDiagnostics(permission.Check(input))
}

func checkWorkflowPermissions(w declaration.WorkflowView) []reporting.Finding {
	return checkPermissionMapping(w.Permissions())
}

func checkJobPermissions(job declaration.JobView) []reporting.Finding {
	return checkPermissionMapping(job.Permissions())
}
