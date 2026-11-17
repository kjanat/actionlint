package correctness

import (
	"iter"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/input/source"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// ParallelStepsRule is the single definition of the parallel-steps rule.
var ParallelStepsRule = &define.Rule{
	Name:        "parallel-steps",
	Description: "Validate background-step references and restrictions within parallel groups",
	Category:    "correctness",
	Subcategory: "steps",
	Recommended: true,
	Help:        "Check background-step names and wait/cancel references in the parallel group.",
	Checks:      define.Fixed(define.Checks{Job: ParallelSteps}),
}

// parallelCheck is a rule to check parallel steps: a 'wait' or 'cancel' step must refer to the ID
// of a preceding background step, and a 'parallel' group may only contain 'run' and 'uses' steps
// ('background', 'wait', 'wait-all', 'cancel', and nested 'parallel' steps are not allowed in it).
// https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel/
type parallelCheck struct {
	reporting.Collector
	// background holds the IDs (lower-cased; step IDs are case insensitive) of the 'background: true'
	// steps seen so far in the current job. Steps are visited in order, including steps nested in
	// 'parallel:' groups, so a 'wait'/'cancel' reference found in this set both exists and precedes
	// the reference.
	background map[string]struct{}
	// inParallel identifies direct children of a 'parallel' group.
	// Only run and uses steps are allowed there. Invalid wait and cancel steps
	// receive that finding, and their references are excluded from validation.
	inParallel map[source.Reference]struct{}
}

// ParallelSteps analyzes one complete job independently of visitor order.
func ParallelSteps(job declaration.Job) []reporting.StepFinding {
	rule := &parallelCheck{background: map[string]struct{}{}, inParallel: map[source.Reference]struct{}{}}
	for step := range job.AllSteps() {
		rule.Step = step
		rule.checkStep(step)
	}
	return rule.Findings
}

func (rule *parallelCheck) checkStep(n declaration.Step) {
	// A step that is itself inside a 'parallel' group is already reported by checkParallelChildren as
	// not allowed there, so skip its reference check to avoid a second, redundant error.
	_, insideParallel := rule.inParallel[n.Reference()]
	if e, ok := n.Wait(); ok {
		if !insideParallel {
			for _, name := range e.Names {
				rule.checkRef(name)
			}
		}
	} else if e, ok := n.Cancel(); ok {
		if !insideParallel {
			rule.checkRef(e.Name)
		}
	} else if e, ok := n.ParallelSteps(); ok {
		rule.checkParallelChildren(e)
	}

	// A child of a parallel group cannot become a separately addressable background step. Its
	// "background" key is invalid and the group itself implicitly waits for all children.
	if !insideParallel && n.ID != nil && !n.ID.ContainsExpression() && isBackgroundStep(n) {
		rule.background[strings.ToLower(n.ID.Text())] = struct{}{}
	}
}

// checkParallelChildren checks the steps inside a 'parallel' group. Only 'run' and 'uses' steps are
// allowed there: 'background', 'wait', 'wait-all', 'cancel', and nested 'parallel' steps are not,
// because steps in a 'parallel' group already run in the background with an implicit wait at the end.
func (rule *parallelCheck) checkParallelChildren(steps iter.Seq[declaration.Step]) {
	for s := range steps {
		rule.inParallel[s.Reference()] = struct{}{}
		if s.Background != nil {
			rule.Errorf(s.Background.Pos, "\"background\" is not allowed for a step inside a \"parallel\" group because the group's steps already run in the background")
		}
		if e, ok := s.Wait(); ok {
			kind := "wait"
			if e.All {
				kind = "wait-all"
			}
			rule.Errorf(s.Pos, "%q step is not allowed inside a \"parallel\" group", kind)
		} else if _, ok := s.Cancel(); ok {
			rule.Errorf(s.Pos, "\"cancel\" step is not allowed inside a \"parallel\" group")
		} else if _, ok := s.ParallelSteps(); ok {
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
func isBackgroundStep(s declaration.Step) bool {
	return s.Background != nil && (s.Background.Expression != nil || s.Background.Value)
}
