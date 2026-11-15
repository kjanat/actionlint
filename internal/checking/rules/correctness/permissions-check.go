package correctness

import (
	permission "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/permissions"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func checkPermissionMapping(p *workflowlang.Permissions) []reporting.Finding {
	if p == nil {
		return nil
	}
	input := permission.Declaration{}
	if p.All != nil {
		input.All = &permission.Value{Text: p.All.Value, Start: reporting.Position(p.All.Pos)}
	}
	for _, scope := range p.Scopes {
		input.Scopes = append(input.Scopes, permission.Scope{Name: permission.Value{Text: scope.Name.Text(), Start: reporting.Position(reporting.SourcePosition(scope.Name))}, Level: permission.Value{Text: scope.Value.Text(), Start: reporting.Position(reporting.SourcePosition(scope.Value))}})
	}
	return reporting.FromDiagnostics(permission.Check(input))
}

func checkWorkflowPermissions(w declaration.Workflow) []reporting.Finding {
	return checkPermissionMapping(w.Permissions)
}

func checkJobPermissions(job declaration.Job) []reporting.Finding {
	return checkPermissionMapping(job.Permissions)
}
