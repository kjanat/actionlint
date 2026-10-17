package checks

import (
	"strings"

	"actionlint.kjanat.dev/internal/workflow/schema"

	"actionlint.kjanat.dev/internal/semantics/glob"

	exprlang "actionlint.kjanat.dev/internal/expression"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	workflownames "actionlint.kjanat.dev/internal/workflow/names"
)

type globCheck struct{ findingCollector }

// GlobWorkflow validates patterns in event declarations.
func GlobWorkflow(n *workflowlang.Workflow) []Finding {
	rule := &globCheck{name: "glob"}

	for _, e := range n.On {
		switch e := e.(type) {
		case *workflowlang.WebhookEvent:
			rule.checkGitRefGlobs(e.Branches)
			rule.checkGitRefGlobs(e.BranchesIgnore)
			rule.checkGitRefGlobs(e.Tags)
			rule.checkGitRefGlobs(e.TagsIgnore)
			rule.checkFilePathGlobs(e.Paths)
			rule.checkFilePathGlobs(e.PathsIgnore)
			rule.checkWorkflowNameGlobs(e.Workflows)
		case *workflowlang.ImageVersionEvent:
			for _, v := range e.Versions {
				rule.checkRefGlob(v)
			}
		}
	}
	return rule.out
}

func (rule *globCheck) checkWorkflowNameGlobs(names []*workflowlang.String) {
	allValid, positive := len(names) != 0, false
	for _, name := range names {
		if name == nil {
			allValid = false
			continue
		}
		value, evaluated := name.Value, false
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
		rule.globErrors(errs, name.Pos, name.Quoted && !evaluated)
	}
	if allValid && !positive {
		rule.Error(names[0].Pos, "workflow_run.workflows requires at least one positive workflow-name pattern")
	}
}

func GlobJob(n *workflowlang.Job) []Finding {
	rule := &globCheck{name: "glob"}
	if n.Snapshot == nil {
		return rule.out
	}
	if expression := n.Snapshot.Expression; expression != nil {
		if value, known := workflowlang.WorkflowExpressionLiteral(expression); known {
			if snapshot, ok := value.(map[string]any); ok {
				rule.checkKnownRefGlob(schema.Property(snapshot, "version"), expression.Pos)
			}
		}
	} else if version := n.Snapshot.Version; version != nil && version.ContainsExpression() {
		if value, known := workflowlang.WorkflowExpressionLiteral(version); known {
			rule.checkKnownRefGlob(value, version.Pos)
		}
	} else {
		rule.checkRefGlob(n.Snapshot.Version)
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
