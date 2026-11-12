package expressions

import (
	"maps"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// BeginComposite initializes the expression contexts owned by one action invocation.
// Caller values are unknown; step output declarations are added as steps execute.
func (rule *Checker) BeginComposite(body workflowdecl.ActionBodyView) {
	rule.compositeScript = true
	rule.inputsTy = new(exprtypes.NewMapObjectType(exprtypes.StringType()))
	rule.stepsTy = new(exprtypes.NewMapObjectType(exprtypes.AnyType()))
	rule.matrixTy = new(exprtypes.NewMapObjectType(exprtypes.AnyType()))
	rule.scriptSources = maps.Collect(body.ExpressionSources())
}

// CheckCompositeStep checks invocation-dependent references and script sinks.
// Declaration validation owns syntax, context availability and call diagnostics.
func (rule *Checker) CheckCompositeStep(step workflowdecl.StepView) error {
	mark := len(rule.errs)
	err := rule.VisitStep(step)
	rule.filterCompositeFindings(mark)
	return err
}

func (rule *Checker) filterCompositeFindings(mark int) {
	findings := rule.errs[mark:]
	rule.errs = rule.errs[:mark]
	for _, finding := range findings {
		if compositeFinding(finding.rule, finding.code) {
			rule.errs = append(rule.errs, finding)
		}
	}
}

// CheckCompositeOutputs checks output values against the invocation's final steps.
func (rule *Checker) CheckCompositeOutputs(body workflowdecl.ActionBodyView) {
	mark := len(rule.errs)
	for output := range body.OutputValues() {
		rule.checkString(output, "jobs.<job_id>.outputs.<output_id>")
	}
	rule.filterCompositeFindings(mark)
}

func compositeFinding(rule, code string) bool {
	return rule == "script-injection" || rule == "script-interpolation" || rule == "expression-reference" && code == "unknown-property"
}
