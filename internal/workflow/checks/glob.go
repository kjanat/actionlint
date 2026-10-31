package checks

import (
	"iter"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/workflow/declaration"

	"actionlint.kjanat.dev/v2/internal/workflow/schema"

	"actionlint.kjanat.dev/v2/internal/semantics/glob"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflownames "actionlint.kjanat.dev/v2/internal/workflow/names"
)

type globCheck struct{ findingCollector }

// GlobWorkflow validates patterns in event declarations.
func GlobWorkflow(n declaration.WorkflowView) []Finding {
	rule := &globCheck{name: "glob"}

	for e := range n.On() {
		if webhook, ok := e.WebhookEvent(); ok {
			rule.checkGitRefGlobs(webhook.Branches())
			rule.checkGitRefGlobs(webhook.BranchesIgnore())
			rule.checkGitRefGlobs(webhook.Tags())
			rule.checkGitRefGlobs(webhook.TagsIgnore())
			rule.checkFilePathGlobs(webhook.Paths())
			rule.checkFilePathGlobs(webhook.PathsIgnore())
			rule.checkWorkflowNameGlobs(webhook.Workflows())
		} else if image, ok := e.ImageVersionEvent(); ok {
			for v := range image.Versions() {
				rule.checkRefGlob(v)
			}
		}
	}
	return rule.out
}

func (rule *globCheck) checkWorkflowNameGlobs(values iter.Seq[declaration.StringView]) {
	names := slices.Collect(values)
	allValid, positive := len(names) != 0, false
	for _, name := range names {
		if !name.Present() {
			allValid = false
			continue
		}
		value, evaluated := name.Value(), false
		if literal := exprlang.LiteralExpressionValue(value); literal != nil {
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
		rule.globErrors(errs, viewPosition(name.Pos()), name.Quoted() && !evaluated)
	}
	if allValid && !positive {
		rule.Error(viewPosition(names[0].Pos()), "workflow_run.workflows requires at least one positive workflow-name pattern")
	}
}

func GlobJob(n declaration.JobView) []Finding {
	rule := &globCheck{name: "glob"}
	if !n.Snapshot().Present() {
		return rule.out
	}
	if expression := n.Snapshot().Expression(); expression.Present() {
		if value, known := expressionLiteral(expression); known {
			if snapshot, ok := value.(map[string]any); ok {
				rule.checkKnownRefGlob(schema.Property(snapshot, "version"), viewPosition(expression.Pos()))
			}
		}
	} else if version := n.Snapshot().Version(); version.Present() && version.ContainsExpression() {
		if value, known := expressionLiteral(version); known {
			rule.checkKnownRefGlob(value, viewPosition(version.Pos()))
		}
	} else {
		rule.checkRefGlob(n.Snapshot().Version())
	}
	return rule.out
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

func (rule *globCheck) checkRefGlob(s declaration.StringView) {
	// Empty value is already checked by parser. Avoid duplicate errors
	if !s.Present() || s.Value() == "" {
		return
	}
	rule.globErrors(glob.ValidateRefGlob(s.Value()), viewPosition(s.Pos()), s.Quoted())
}

func (rule *globCheck) checkGitRefGlobs(filter declaration.WebhookEventFilterView) {
	if !filter.Present() {
		return
	}
	for v := range filter.Values() {
		rule.checkRefGlob(v)
	}
}

func (rule *globCheck) checkFilePathGlobs(filter declaration.WebhookEventFilterView) {
	if !filter.Present() {
		return
	}
	for v := range filter.Values() {
		// Empty value is already checked by parser. Avoid duplicate errors
		if v.Value() != "" {
			rule.globErrors(glob.ValidatePathGlob(v.Value()), viewPosition(v.Pos()), v.Quoted())
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
