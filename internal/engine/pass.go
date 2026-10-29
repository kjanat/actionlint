package engine

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"time"

	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// Visitor visits syntax tree from root in depth-first order
type Visitor struct {
	passes []workflowvisit.Pass
	dbg    io.Writer
	Hooks  Hooks
}

// Hooks lets a caller observe document/job entry and own step expansion.
// ParallelBoundary invalidates caller-owned assumptions between parallel branches.
type Hooks struct {
	Workflow         func(declaration.WorkflowView) error
	Job              func(declaration.JobView) error
	Step             func(declaration.StepView) error
	ParallelBoundary func()
}

// NewVisitor creates Visitor instance
func NewVisitor() *Visitor {
	return &Visitor{}
}

// AddPass adds new pass which is called on traversing a syntax tree
func (v *Visitor) AddPass(p workflowvisit.Pass) {
	v.passes = append(v.passes, p)
}

// EnableDebug enables debug output when non-nil io.Writer value is given. All debug outputs from
// visitor will be written to the writer.
func (v *Visitor) EnableDebug(w io.Writer) {
	v.dbg = w
}

func (v *Visitor) reportElapsedTime(what string, start time.Time) {
	_, _ = fmt.Fprintf(v.dbg, "[Visitor] %s took %vms\n", what, time.Since(start).Milliseconds())
}

// Visit visits given syntax tree in depth-first order
func (v *Visitor) Visit(n declaration.WorkflowView) error {
	var t time.Time
	if v.dbg != nil {
		t = time.Now()
	}

	if v.Hooks.Workflow != nil {
		if err := v.Hooks.Workflow(n); err != nil {
			return err
		}
	}
	for _, p := range v.passes {
		if err := p.VisitWorkflowPre(n); err != nil {
			return err
		}
	}

	if v.dbg != nil {
		v.reportElapsedTime("VisitWorkflowPre", t)
		t = time.Now()
	}

	// Stable source order also makes reusable metadata findings select the first
	// visible caller consistently, including calls through nested composites.
	jobs := slices.SortedFunc(func(yield func(declaration.JobView) bool) {
		for _, job := range n.Jobs() {
			if !yield(job) {
				return
			}
		}
	}, func(a, b declaration.JobView) int {
		left, leftPresent := a.Pos()
		right, rightPresent := b.Pos()
		switch {
		case leftPresent && rightPresent:
			if order := cmp.Compare(left.Line, right.Line); order != 0 {
				return order
			}
			if order := cmp.Compare(left.Col, right.Col); order != 0 {
				return order
			}
		case leftPresent:
			return -1
		case rightPresent:
			return 1
		}
		return cmp.Compare(a.Key(), b.Key())
	})
	for _, job := range jobs {
		if err := v.visitJob(job); err != nil {
			return err
		}
	}

	if v.dbg != nil {
		v.reportElapsedTime(fmt.Sprintf("Visiting %d jobs", len(jobs)), t)
		t = time.Now()
	}

	for _, p := range v.passes {
		if err := p.VisitWorkflowPost(n); err != nil {
			return err
		}
	}

	if v.dbg != nil {
		v.reportElapsedTime("VisitWorkflowPost", t)
	}

	return nil
}

func (v *Visitor) visitJob(n declaration.JobView) error {
	if v.Hooks.Job != nil {
		if err := v.Hooks.Job(n); err != nil {
			return err
		}
	}
	var t time.Time
	if v.dbg != nil {
		t = time.Now()
	}

	for _, p := range v.passes {
		if err := p.VisitJobPre(n); err != nil {
			return err
		}
	}

	if v.dbg != nil {
		v.reportElapsedTime(fmt.Sprintf("VisitWorkflowJobPre at job %q", n.ID().Value()), t)
		t = time.Now()
	}

	stepCount := 0
	for s := range n.Steps() {
		stepCount++
		if err := v.visitStep(s); err != nil {
			return err
		}
	}

	if v.dbg != nil {
		v.reportElapsedTime(fmt.Sprintf("Visiting %d steps at job %q", stepCount, n.ID().Value()), t)
		t = time.Now()
	}

	for _, p := range v.passes {
		if err := p.VisitJobPost(n); err != nil {
			return err
		}
	}

	if v.dbg != nil {
		v.reportElapsedTime(fmt.Sprintf("VisitWorkflowJobPost at job %q", n.ID().Value()), t)
	}

	return nil
}

func (v *Visitor) visitStep(n declaration.StepView) error {
	var t time.Time
	if v.dbg != nil {
		t = time.Now()
	}

	if v.Hooks.Step != nil {
		if err := v.Hooks.Step(n); err != nil {
			return err
		}
	} else {
		for _, p := range v.passes {
			if err := p.VisitStep(n); err != nil {
				return err
			}
		}
	}

	// Steps grouped in a 'parallel' step are visited as well so that rules are applied to them.
	// https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel/
	if e, ok := n.Parallel(); ok {
		for s := range e.Steps() {
			if v.Hooks.ParallelBoundary != nil {
				v.Hooks.ParallelBoundary()
			}
			if err := v.visitStep(s); err != nil {
				return err
			}
		}
		if v.Hooks.ParallelBoundary != nil {
			v.Hooks.ParallelBoundary()
		}
	}

	if v.dbg != nil {
		v.reportElapsedTime("VisitStep "+n.Reference().Node, t)
	}

	return nil
}
