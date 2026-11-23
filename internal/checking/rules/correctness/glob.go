package correctness

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness/glob"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/model/workflows/names"
	"actionlint.kjanat.dev/v2/internal/model/workflows/schema"
)

var GlobRule = &define.Rule{
	Name:        "glob",
	Description: "Validate branch, tag, and path filter patterns",
	Category:    "correctness",
	Subcategory: "filters",
	Recommended: true,
	Help:        "Fix the pattern syntax or the order of positive and negative patterns.",
	Checks:      define.Fixed(define.Checks{Workflow: GlobWorkflow, Job: GlobJob}),
}

type globCheck struct{ reporting.Collector }

func GlobWorkflow(n workflows.BoundWorkflow) []reporting.StepFinding {
	rule := &globCheck{}

	for _, e := range n.On {
		if webhook, ok := e.(*definition.WebhookEvent); ok {
			rule.checkGitRefGlobs(webhook.Branches)
			rule.checkGitRefGlobs(webhook.BranchesIgnore)
			rule.checkGitRefGlobs(webhook.Tags)
			rule.checkGitRefGlobs(webhook.TagsIgnore)
			rule.checkFilePathGlobs(webhook.Paths)
			rule.checkFilePathGlobs(webhook.PathsIgnore)
			rule.checkWorkflowNameGlobs(webhook.Workflows)
		} else if image, ok := e.(*definition.ImageVersionEvent); ok {
			for _, v := range image.Versions {
				rule.checkRefGlob(v)
			}
		}
	}
	return rule.Findings
}

func (rule *globCheck) checkWorkflowNameGlobs(workflowNames []*definition.String) {
	allValid, positive := len(workflowNames) != 0, false
	for _, name := range workflowNames {
		if name == nil {
			allValid = false
			continue
		}
		value, evaluated := name.Value, false
		if literal := syntax.LiteralExpressionValue(value); literal != nil {
			value, evaluated = *literal, true
		} else if name.ContainsExpression() {
			allValid = false
			continue
		}
		if names.ValidPattern(value) {
			positive = positive || !strings.HasPrefix(value, "!")
			continue
		}
		allValid = false
		if value == "" && !evaluated {
			continue
		}
		errs := glob.Validate(value, false)
		if len(errs) == 0 {
			errs = []glob.InvalidGlobPatternError{{Message: "invalid workflow-name filter pattern"}}
		}
		if evaluated {
			for i := range errs {
				errs[i].Column = 0
			}
		}
		rule.globErrors(errs, name.Pos, name.Quoted && !evaluated)
	}
	if allValid && !positive {
		rule.Error(workflowNames[0].Pos, "workflow_run.workflows requires at least one positive workflow-name pattern")
	}
}

func GlobJob(n workflows.BoundJob) []reporting.StepFinding {
	rule := &globCheck{}
	if n.Snapshot == nil {
		return rule.Findings
	}
	if expression := n.Snapshot.Expression; expression != nil {
		if value, known := expression.ConstantValue(); known {
			if snapshot, ok := value.(map[string]any); ok {
				rule.checkKnownRefGlob(schema.Property(snapshot, "version"), expression.Pos)
			}
		}
	} else if version := n.Snapshot.Version; version != nil && version.ContainsExpression() {
		if value, known := version.ConstantValue(); known {
			rule.checkKnownRefGlob(value, version.Pos)
		}
	} else {
		rule.checkRefGlob(n.Snapshot.Version)
	}
	return rule.Findings
}

func (rule *globCheck) checkKnownRefGlob(value any, pos *sourcetext.Position) {
	pattern, known := definition.WorkflowScalarString(value)
	if !known || pattern == "" {
		return
	}
	errs := glob.ValidateRefGlob(pattern)
	for i := range errs {
		errs[i].Column = 0 // Evaluated offsets refer to the result; report the source expression.
	}
	rule.globErrors(errs, pos, false)
}

func (rule *globCheck) checkRefGlob(s *definition.String) {
	// The parser reports empty values.
	if s == nil || s.Value == "" {
		return
	}
	rule.globErrors(glob.ValidateRefGlob(s.Value), s.Pos, s.Quoted)
}

func (rule *globCheck) checkGitRefGlobs(filter *definition.WebhookEventFilter) {
	if filter == nil {
		return
	}
	for _, v := range filter.Values {
		rule.checkRefGlob(v)
	}
}

func (rule *globCheck) checkFilePathGlobs(filter *definition.WebhookEventFilter) {
	if filter == nil {
		return
	}
	for _, v := range filter.Values {
		// The parser reports empty values.
		if v.Value != "" {
			rule.globErrors(glob.ValidatePathGlob(v.Value), v.Pos, v.Quoted)
		}
	}
}

func (rule *globCheck) globErrors(errs []glob.InvalidGlobPatternError, pos *sourcetext.Position, quoted bool) {
	for i := range errs {
		err := &errs[i]
		p := *pos
		if quoted {
			p.Column++
		}
		if err.Column != 0 {
			p.Column += err.Column - 1
		}
		rule.Errorf(&p, "%s. note: filter pattern syntax is explained at https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#filter-pattern-cheat-sheet", err.Message)
	}
}
