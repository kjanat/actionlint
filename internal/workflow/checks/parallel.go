package checks

import (
	"strings"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

// parallelCheck is a rule to check parallel steps: a 'wait' or 'cancel' step must refer to the ID
// of a preceding background step, and a 'parallel' group may only contain 'run' and 'uses' steps
// ('background', 'wait', 'wait-all', 'cancel', and nested 'parallel' steps are not allowed in it).
// https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel/
type parallelCheck struct {
	findingCollector
	// background holds the IDs (lower-cased; step IDs are case insensitive) of the 'background: true'
	// steps seen so far in the current job. Steps are visited in order, including steps nested in
	// 'parallel:' groups, so a 'wait'/'cancel' reference found in this set both exists and precedes
	// the reference.
	background map[string]struct{}
	// inParallel holds the steps that are direct children of a 'parallel' group. Such steps are not
	// allowed there at all (only 'run'/'uses' are), so their 'wait'/'cancel' references are not
	// separately validated: the "not allowed inside a parallel group" error stands on its own
	// instead of cascading into a redundant "unknown reference" error from the same step.
	inParallel map[*workflowlang.Step]struct{}
}

// ParallelSteps analyzes one complete job independently of visitor order.
func ParallelSteps(job *workflowlang.Job) []Finding {
	rule := &parallelCheck{name: "parallel-steps", background: map[string]struct{}{}, inParallel: map[*workflowlang.Step]struct{}{}}
	var visit func([]*workflowlang.Step)
	visit = func(steps []*workflowlang.Step) {
		for _, step := range steps {
			rule.step = step
			rule.checkStep(step)
			if parallel, ok := step.Exec.(*workflowlang.ExecParallel); ok {
				visit(parallel.Steps)
			}
		}
	}
	visit(job.Steps)
	return rule.out
}

func (rule *parallelCheck) checkStep(n *workflowlang.Step) {
	// A step that is itself inside a 'parallel' group is already reported by checkParallelChildren as
	// not allowed there, so skip its reference check to avoid a second, redundant error.
	_, insideParallel := rule.inParallel[n]
	switch e := n.Exec.(type) {
	case *workflowlang.ExecWait:
		if !insideParallel {
			for _, name := range e.Names {
				rule.checkRef(name)
			}
		}
	case *workflowlang.ExecCancel:
		if !insideParallel {
			rule.checkRef(e.Name)
		}
	case *workflowlang.ExecParallel:
		rule.checkParallelChildren(e.Steps)
	}

	// A child of a parallel group cannot become a separately addressable background step. Its
	// "background" key is invalid and the group itself implicitly waits for all children.
	if !insideParallel && n.ID != nil && !n.ID.ContainsExpression() && isBackgroundStep(n) {
		rule.background[strings.ToLower(n.ID.Value)] = struct{}{}
	}
}

// checkParallelChildren checks the steps inside a 'parallel' group. Only 'run' and 'uses' steps are
// allowed there: 'background', 'wait', 'wait-all', 'cancel', and nested 'parallel' steps are not,
// because steps in a 'parallel' group already run in the background with an implicit wait at the end.
func (rule *parallelCheck) checkParallelChildren(steps []*workflowlang.Step) {
	for _, s := range steps {
		rule.inParallel[s] = struct{}{}
		if s.Background != nil {
			rule.Errorf(s.Background.Pos, "\"background\" is not allowed for a step inside a \"parallel\" group because the group's steps already run in the background")
		}
		switch e := s.Exec.(type) {
		case *workflowlang.ExecWait:
			kind := "wait"
			if e.All {
				kind = "wait-all"
			}
			rule.Errorf(s.Pos, "%q step is not allowed inside a \"parallel\" group", kind)
		case *workflowlang.ExecCancel:
			rule.Errorf(s.Pos, "\"cancel\" step is not allowed inside a \"parallel\" group")
		case *workflowlang.ExecParallel:
			rule.Errorf(s.Pos, "\"parallel\" step cannot be nested in another \"parallel\" step")
		}
	}
}

func (rule *parallelCheck) checkRef(ref *workflowlang.String) {
	if ref == nil || ref.Value == "" || ref.ContainsExpression() {
		// Empty values come from an already-reported parse error; expression step IDs can't be
		// resolved statically. Skip both.
		return
	}
	if _, ok := rule.background[strings.ToLower(ref.Value)]; !ok {
		rule.Errorf(
			ref.Pos,
			"%q is not the ID of a preceding background step. \"wait\" and \"cancel\" steps can only refer to an earlier step that has \"background: true\"",
			ref.Value,
		)
	}
}

// isBackgroundStep reports whether the step runs (or may run) in the background. When 'background' is
// an expression, whether the step runs in the background can't be known statically, so it's treated
// as a possible background step to avoid false positives.
func isBackgroundStep(s *workflowlang.Step) bool {
	return s.Background != nil && (s.Background.Expression != nil || s.Background.Value)
}
