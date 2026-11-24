package correctness

import (
	"iter"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

var ParallelStepsRule = &define.Rule{
	Name:        "parallel-steps",
	Description: "Validate background-step references and restrictions within parallel groups",
	Category:    "correctness",
	Subcategory: "steps",
	Recommended: true,
	Help:        "Fix the background-step names and the wait and cancel references.",
	Checks:      define.Fixed(define.Checks{Job: ParallelSteps}),
}

type parallelCheck struct {
	reporting.Collector
	// background holds lowercase IDs of the 'background: true' steps seen so far.
	// Steps are visited in source order, so a reference found here also follows its step.
	background map[string]struct{}
	inParallel map[sourcetext.Reference]struct{}
}

func ParallelSteps(job workflows.BoundJob) []reporting.StepFinding {
	rule := &parallelCheck{background: map[string]struct{}{}, inParallel: map[sourcetext.Reference]struct{}{}}
	for step := range job.AllSteps() {
		rule.Step = step
		rule.checkStep(step)
	}
	return rule.Findings
}

func (rule *parallelCheck) checkStep(n workflows.BoundStep) {
	// checkParallelChildren already reports a wait or cancel step inside a 'parallel' group.
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

func (rule *parallelCheck) checkParallelChildren(steps iter.Seq[workflows.BoundStep]) {
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

func (rule *parallelCheck) checkRef(ref *definition.String) {
	if ref == nil || ref.Value == "" || ref.ContainsExpression() {
		// Empty values come from an already reported parse error, and expression step IDs
		// cannot be resolved statically.
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

// isBackgroundStep treats an expression-valued 'background' as a possible background step.
func isBackgroundStep(s workflows.BoundStep) bool {
	return s.Background != nil && (s.Background.Expression != nil || s.Background.Value)
}
