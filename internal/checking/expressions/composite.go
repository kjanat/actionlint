package expressions

import (
	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"

	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

func (rule *Visitor) BeginComposite(body workflows.ActionBody) {
	rule.compositeScript = true
	rule.inputsTy = new(exprtypes.NewMapObjectType(exprtypes.StringType()))
	rule.stepsTy = new(exprtypes.NewMapObjectType(exprtypes.AnyType()))
	rule.matrixTy = new(exprtypes.NewMapObjectType(exprtypes.AnyType()))
	rule.scriptSources = body.ExpressionSources()
}

func (rule *Visitor) CheckCompositeStep(step workflows.BoundStep) error {
	mark := len(rule.errs)
	err := rule.VisitStep(step)
	rule.filterCompositeFindings(mark)
	return err
}

func (rule *Visitor) filterCompositeFindings(mark int) {
	findings := rule.errs[mark:]
	rule.errs = rule.errs[:mark]
	for _, finding := range findings {
		if compositeFinding(finding.rule, finding.code) {
			rule.errs = append(rule.errs, finding)
		}
	}
}

func (rule *Visitor) CheckCompositeOutputs(body workflows.ActionBody) {
	mark := len(rule.errs)
	for output := range body.OutputValues() {
		rule.checkString(output, "jobs.<job_id>.outputs.<output_id>")
	}
	rule.filterCompositeFindings(mark)
}

func compositeFinding(rule, code string) bool {
	return rule == "script-injection" || rule == "script-interpolation" || rule == "expression-reference" && code == "unknown-property"
}
