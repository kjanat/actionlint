package engine

import (
	"path"
	"strings"

	"actionlint.kjanat.dev/internal/repoconfig"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// actionUse is one "uses:" value of an action step found while visiting a workflow.
type actionUse struct {
	spec string
	name string
	ref  string
	pos  *workflowlang.Pos
}

// matchGlob reports whether the value matches the pattern. An invalid pattern matches nothing, so a
// policy built through the Go API with a broken pattern reports its action as missing instead of
// silently passing.
func matchGlob(pattern, value string) bool {
	ok, err := path.Match(pattern, value)
	return err == nil && ok
}

// Jobs are stored in a map. Source order keeps diagnostics stable across runs.
func firstStepsJobPos(w *workflowlang.Workflow) *workflowlang.Pos {
	var ret *workflowlang.Pos
	for _, j := range w.Jobs {
		if j == nil || j.Pos == nil || j.WorkflowCall != nil {
			continue
		}
		if ret == nil || j.Pos.IsBefore(ret) {
			ret = j.Pos
		}
	}
	return ret
}

// RuleRequiredActions is a rule to check that a workflow uses the actions which the repository
// requires. The "required-actions" policy in the configuration file enables it and lists the actions.
type RuleRequiredActions struct {
	RuleBase
	ruleConfiguration
	uses    []actionUse
	unknown bool
}

// NewRuleRequiredActions creates a new RuleRequiredActions instance.
func NewRuleRequiredActions() *RuleRequiredActions {
	return &RuleRequiredActions{
		RuleBase: builtinRuleBase("required-actions"),
	}
}

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (rule *RuleRequiredActions) VisitWorkflowPre(n *workflowlang.Workflow) error {
	rule.uses = nil
	rule.unknown = false
	return nil
}

// VisitStep is callback when visiting Step node.
func (rule *RuleRequiredActions) VisitStep(n *workflowlang.Step) error {
	e, ok := n.Exec.(*workflowlang.ExecAction)
	if !ok || e.Uses == nil {
		return nil
	}
	if e.Uses.ContainsExpression() {
		rule.unknown = true
		return nil
	}
	name, ref := repoconfig.SplitActionRef(e.Uses.Value)
	rule.uses = append(rule.uses, actionUse{
		spec: e.Uses.Value,
		name: name,
		ref:  ref,
		pos:  e.Uses.Pos,
	})
	return nil
}

// VisitWorkflowPost is callback when visiting Workflow node after visiting its children.
func (rule *RuleRequiredActions) VisitWorkflowPost(n *workflowlang.Workflow) error {
	if rule.unknown {
		return nil
	}
	pos := firstStepsJobPos(n)
	if pos == nil {
		return nil
	}
	for _, a := range rule.config.RequiredActions() {
		rule.report(a, pos)
	}
	return nil
}

func (rule *RuleRequiredActions) report(required string, pos *workflowlang.Pos) {
	name, ref := repoconfig.SplitActionRef(required)
	name = strings.ToLower(name)

	var near *actionUse
	for i, u := range rule.uses {
		if !matchGlob(name, strings.ToLower(u.name)) {
			continue
		}
		if ref == "" || matchGlob(ref, u.ref) {
			return
		}
		if near == nil || u.pos.IsBefore(near.pos) {
			near = &rule.uses[i]
		}
	}

	if near != nil {
		rule.Errorf(
			pos,
			"no step in this workflow uses action %q which is required by the \"required-actions\" policy in actionlint.yaml. %q at %s matches the action name but not the required ref",
			required,
			near.spec,
			near.pos,
		)
		return
	}

	rule.Errorf(
		pos,
		"no step in this workflow uses action %q which is required by the \"required-actions\" policy in actionlint.yaml",
		required,
	)
}
