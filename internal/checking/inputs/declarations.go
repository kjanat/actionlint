package inputs

import (
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// DeclarationCheck binds rule metadata to checks of published workflow views.
// A nil callback requests no input of that kind. Each callback returns independent
// findings and may run without any preceding or following callback.
type DeclarationCheck struct {
	rule.Declaration
	Workflow func(declaration.WorkflowView) []reporting.Finding
	Job      func(declaration.JobView) []reporting.Finding
	Step     func(declaration.StepView) []reporting.Finding
}
