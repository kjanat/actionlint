package parser

import workflowlang "actionlint.kjanat.dev/v2/internal/workflow"

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
		}
	}
	var steps func([]*workflowlang.Step)
	steps = func(nodes []*workflowlang.Step) {
		for _, step := range nodes {
			switch exec := step.Exec.(type) {
			case *workflowlang.ExecAction:
				add(exec.Uses)
			case *workflowlang.ExecParallel:
				steps(exec.Steps)
			}
		}
	}
	for _, job := range workflow.Jobs {
		if job.WorkflowCall != nil {
			add(job.WorkflowCall.Uses)
		}
		steps(job.Steps)
	}
}
