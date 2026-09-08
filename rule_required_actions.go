package actionlint

import (
	"path"
	"strings"
)

// actionUse is one "uses:" value of an action step found while visiting a workflow.
type actionUse struct {
	spec string
	name string
	ref  string
	pos  *Pos
}

// splitActionRef splits an action reference, or a pattern for one, into its name and its ref at the
// first "@". The ref is empty when the value contains no "@".
func splitActionRef(s string) (string, string) {
	name, ref, _ := strings.Cut(s, "@")
	return name, ref
}

// invalidActionPattern returns the half of the entry which path.Match rejects as a pattern. The
// second return value is false when both halves are valid.
func invalidActionPattern(s string) (string, bool) {
	name, ref := splitActionRef(s)
	for _, p := range []string{name, ref} {
		if _, err := path.Match(p, ""); err != nil {
			return p, true
		}
	}
	return "", false
}

// matchGlob reports whether the value matches the pattern. An invalid pattern matches nothing, so a
// policy built through the Go API with a broken pattern reports its action as missing instead of
// silently passing.
func matchGlob(pattern, value string) bool {
	ok, err := path.Match(pattern, value)
	return err == nil && ok
}

// Jobs are stored in a map. Source order keeps diagnostics stable across runs.
func firstStepsJobPos(w *Workflow) *Pos {
	var ret *Pos
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
	uses    []actionUse
	unknown bool
}

// NewRuleRequiredActions creates a new RuleRequiredActions instance.
func NewRuleRequiredActions() *RuleRequiredActions {
	return &RuleRequiredActions{
		RuleBase: RuleBase{
			name: "required-actions",
			desc: "Checks that the actions listed in the \"required-actions\" policy in actionlint.yaml are used",
		},
	}
}

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (rule *RuleRequiredActions) VisitWorkflowPre(n *Workflow) error {
	rule.uses = nil
	rule.unknown = false
	return nil
}

// VisitStep is callback when visiting Step node.
func (rule *RuleRequiredActions) VisitStep(n *Step) error {
	e, ok := n.Exec.(*ExecAction)
	if !ok || e.Uses == nil {
		return nil
	}
	if e.Uses.ContainsExpression() {
		rule.unknown = true
		return nil
	}
	name, ref := splitActionRef(e.Uses.Value)
	rule.uses = append(rule.uses, actionUse{
		spec: e.Uses.Value,
		name: name,
		ref:  ref,
		pos:  e.Uses.Pos,
	})
	return nil
}

// VisitWorkflowPost is callback when visiting Workflow node after visiting its children.
func (rule *RuleRequiredActions) VisitWorkflowPost(n *Workflow) error {
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

func (rule *RuleRequiredActions) report(required string, pos *Pos) {
	name, ref := splitActionRef(required)
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
