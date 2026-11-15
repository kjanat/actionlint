package correctness

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	workflowvalues "actionlint.kjanat.dev/v2/internal/model/workflows/values"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"

	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness/glob"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/schema"

	workflownames "actionlint.kjanat.dev/v2/internal/model/workflows/names"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

type globCheck struct{ reporting.Collector }

// GlobWorkflow validates patterns in event declarations.
func GlobWorkflow(n declaration.Workflow) []reporting.Finding {
	rule := &globCheck{Rule: "glob"}

	for _, e := range n.On {
		if webhook, ok := e.(*workflowlang.WebhookEvent); ok {
			rule.checkGitRefGlobs(webhook.Branches)
			rule.checkGitRefGlobs(webhook.BranchesIgnore)
			rule.checkGitRefGlobs(webhook.Tags)
			rule.checkGitRefGlobs(webhook.TagsIgnore)
			rule.checkFilePathGlobs(webhook.Paths)
			rule.checkFilePathGlobs(webhook.PathsIgnore)
			rule.checkWorkflowNameGlobs(webhook.Workflows)
		} else if image, ok := e.(*workflowlang.ImageVersionEvent); ok {
			for _, v := range image.Versions {
				rule.checkRefGlob(v)
			}
		}
	}
	return rule.Findings
}

func (rule *globCheck) checkWorkflowNameGlobs(names []*workflowlang.String) {
	allValid, positive := len(names) != 0, false
	for _, name := range names {
		if name == nil {
			allValid = false
			continue
		}
		value, evaluated := name.Value, false
		if literal := exprsyntax.LiteralExpressionValue(value); literal != nil {
			value, evaluated = *literal, true
		} else if name.ContainsExpression() {
			allValid = false
			continue
		}
		if workflownames.ValidPattern(value) {
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
		rule.Error(names[0].Pos, "workflow_run.workflows requires at least one positive workflow-name pattern")
	}
}

func GlobJob(n declaration.Job) []reporting.Finding {
	rule := &globCheck{Rule: "glob"}
	if n.Snapshot == nil {
		return rule.Findings
	}
	if expression := n.Snapshot.Expression; expression != nil {
		if value, known := workflowvalues.Constant(expression); known {
			if snapshot, ok := value.(map[string]any); ok {
				rule.checkKnownRefGlob(schema.Property(snapshot, "version"), expression.Pos)
			}
		}
	} else if version := n.Snapshot.Version; version != nil && version.ContainsExpression() {
		if value, known := workflowvalues.Constant(version); known {
			rule.checkKnownRefGlob(value, version.Pos)
		}
	} else {
		rule.checkRefGlob(n.Snapshot.Version)
	}
	return rule.Findings
}

func (rule *globCheck) checkKnownRefGlob(value any, pos *workflowlang.Pos) {
	pattern, known := workflowlang.WorkflowScalarString(value)
	if !known || pattern == "" {
		return
	}
	errs := glob.ValidateRefGlob(pattern)
	for i := range errs {
		errs[i].Column = 0 // Evaluated offsets refer to the result; report the source expression.
	}
	rule.globErrors(errs, pos, false)
}

func (rule *globCheck) checkRefGlob(s *workflowlang.String) {
	// Empty value is already checked by parser. Avoid duplicate errors
	if s == nil || s.Value == "" {
		return
	}
	rule.globErrors(glob.ValidateRefGlob(s.Value), s.Pos, s.Quoted)
}

func (rule *globCheck) checkGitRefGlobs(filter *workflowlang.WebhookEventFilter) {
	if filter == nil {
		return
	}
	for _, v := range filter.Values {
		rule.checkRefGlob(v)
	}
}

func (rule *globCheck) checkFilePathGlobs(filter *workflowlang.WebhookEventFilter) {
	if filter == nil {
		return
	}
	for _, v := range filter.Values {
		// Empty value is already checked by parser. Avoid duplicate errors
		if v.Value != "" {
			rule.globErrors(glob.ValidatePathGlob(v.Value), v.Pos, v.Quoted)
		}
	}
}

func (rule *globCheck) globErrors(errs []glob.InvalidGlobPatternError, pos *workflowlang.Pos, quoted bool) {
	for i := range errs {
		err := &errs[i]
		p := *pos
		if quoted {
			p.Col++
		}
		if err.Column != 0 {
			p.Col += err.Column - 1
		}
		rule.Errorf(&p, "%s. note: filter pattern syntax is explained at https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#filter-pattern-cheat-sheet", err.Message)
	}
}
