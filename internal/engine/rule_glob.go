package engine

import (
	"strings"

	exprlang "actionlint.kjanat.dev/internal/expression"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflownames"
)

// RuleGlob is a rule to check glob syntax.
// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#filter-pattern-cheat-sheet
type RuleGlob struct {
	RuleBase
}

// NewRuleGlob creates new RuleGlob instance.
func NewRuleGlob() *RuleGlob {
	return &RuleGlob{
		RuleBase: builtinRuleBase("glob"),
	}
}

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (rule *RuleGlob) VisitWorkflowPre(n *workflowlang.Workflow) error {
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
	return nil
}

func (rule *RuleGlob) checkWorkflowNameGlobs(names []*workflowlang.String) {
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
		errs := validateGlob(value, false)
		if len(errs) == 0 {
			errs = []InvalidGlobPatternError{{Message: "invalid workflow-name filter pattern"}}
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

func (rule *RuleGlob) VisitJobPre(n *workflowlang.Job) error {
	if n.Snapshot == nil {
		return nil
	}
	if expression := n.Snapshot.Expression; expression != nil {
		if value, known := workflowlang.WorkflowExpressionLiteral(expression); known {
			if snapshot, ok := value.(map[string]any); ok {
				rule.checkKnownRefGlob(workflowObjectProperty(snapshot, "version"), expression.Pos)
			}
		}
	} else if version := n.Snapshot.Version; version != nil && version.ContainsExpression() {
		if value, known := workflowlang.WorkflowExpressionLiteral(version); known {
			rule.checkKnownRefGlob(value, version.Pos)
		}
	} else {
		rule.checkRefGlob(n.Snapshot.Version)
	}
	return nil
}

func (rule *RuleGlob) checkKnownRefGlob(value any, pos *workflowlang.Pos) {
	pattern, known := workflowlang.WorkflowScalarString(value)
	if !known || pattern == "" {
		return
	}
	errs := ValidateRefGlob(pattern)
	for i := range errs {
		errs[i].Column = 0 // Evaluated offsets refer to the result; report the source expression.
	}
	rule.globErrors(errs, pos, false)
}

func (rule *RuleGlob) checkRefGlob(s *workflowlang.String) {
	// Empty value is already checked by parser. Avoid duplicate errors
	if s == nil || s.Value == "" {
		return
	}
	rule.globErrors(ValidateRefGlob(s.Value), s.Pos, s.Quoted)
}

func (rule *RuleGlob) checkGitRefGlobs(filter *workflowlang.WebhookEventFilter) {
	if filter == nil {
		return
	}
	for _, v := range filter.Values {
		rule.checkRefGlob(v)
	}
}

func (rule *RuleGlob) checkFilePathGlobs(filter *workflowlang.WebhookEventFilter) {
	if filter == nil {
		return
	}
	for _, v := range filter.Values {
		// Empty value is already checked by parser. Avoid duplicate errors
		if v.Value != "" {
			rule.globErrors(ValidatePathGlob(v.Value), v.Pos, v.Quoted)
		}
	}
}

func (rule *RuleGlob) globErrors(errs []InvalidGlobPatternError, pos *workflowlang.Pos, quoted bool) {
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
