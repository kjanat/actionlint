package checks

import (
	"iter"
	"path"
	"strings"

	"actionlint.kjanat.dev/v2/internal/workflow/declaration"

	"actionlint.kjanat.dev/v2/internal/workflow/references"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
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
func firstStepsJobPos(w declaration.WorkflowView) *workflowlang.Pos {
	var ret *workflowlang.Pos
	for _, j := range w.Jobs() {
		if !j.Present() || viewPosition(j.Pos()) == nil || j.WorkflowCall().Present() {
			continue
		}
		if ret == nil || viewPosition(j.Pos()).IsBefore(ret) {
			ret = viewPosition(j.Pos())
		}
	}
	return ret
}

type requiredActionsCheck struct {
	findingCollector
	uses    []actionUse
	unknown bool
}

// RequiredActions evaluates all action invocations from one workflow snapshot.
func RequiredActions(w declaration.WorkflowView, required []string) []Finding {
	rule := &requiredActionsCheck{name: "required-actions"}
	pos := firstStepsJobPos(w)
	if pos == nil || len(required) == 0 {
		return nil
	}
	var visit func(iter.Seq[declaration.StepView])
	visit = func(steps iter.Seq[declaration.StepView]) {
		for step := range steps {
			rule.checkStep(step)
			if group, ok := step.Parallel(); ok {
				visit(group.Steps())
			}
		}
	}
	for _, job := range w.Jobs() {
		if job.Present() {
			visit(job.Steps())
		}
	}
	if rule.unknown {
		return nil
	}
	for _, spec := range required {
		rule.report(spec, pos)
	}
	return rule.out
}

func (rule *requiredActionsCheck) checkStep(n declaration.StepView) {
	e, ok := n.Action()
	if !ok || !e.Uses().Present() {
		return
	}
	if e.Uses().ContainsExpression() {
		rule.unknown = true
		return
	}
	name, ref := references.SplitRef(e.Uses().Value())
	rule.uses = append(rule.uses, actionUse{
		spec: e.Uses().Value(),
		name: name,
		ref:  ref,
		pos:  viewPosition(e.Uses().Pos()),
	})
}

func (rule *requiredActionsCheck) report(required string, pos *workflowlang.Pos) {
	name, ref := references.SplitRef(required)
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
