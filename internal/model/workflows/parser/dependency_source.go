package parser

import (
	"actionlint.kjanat.dev/v2/internal/input/source"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// IndexDependencySpans records physical scalar ends before the decoded model
// leaves parsing. Aliases reuse the cached source mapping of their declaration.
// IndexScalarSources supplies the physical graph; unavailable spans stay unset.
func (p *Parser) IndexDependencySpans(workflow *workflowlang.Workflow) {
	if workflow == nil || p.scalarDocument == nil || p.scalarSources == nil {
		return
	}
	add := func(value *workflowlang.String) {
		if value == nil || value.ContainsExpression() || !value.SourceScalar.HasSource {
			return
		}
		if scalar := p.scalarSource(value.SourceScalar.Pos, value.SourceScalar.Value); scalar != nil {
			value.SourceScalar.End = scalar.ScalarEnd
			if workflow.ExpressionSources == nil {
				workflow.ExpressionSources = make(map[source.Position]*workflowlang.ScriptSource)
			}
			workflow.ExpressionSources[value.SourceScalar.Pos] = scalar
		}
	}
	for _, job := range workflow.Jobs {
		if job.WorkflowCall != nil {
			add(job.WorkflowCall.Uses)
		}
		for step := range workflowlang.AllSteps(job.Steps) {
			if exec, ok := step.Exec.(*workflowlang.ExecAction); ok {
				add(exec.Uses)
			}
		}
	}
}
