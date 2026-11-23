package workflowvisit

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"time"

	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

type Options struct {
	// Passes run in slice order at every node.
	Passes []Pass
	Hooks  Hooks
	Debug  io.Writer
}

// Hooks JobPass runs each pass's VisitJobPre and VisitJobPost call through visit.
// ParallelBoundary runs before each parallel branch and after the last one.
type Hooks struct {
	Workflow         func(workflows.BoundWorkflow) error
	Job              func(workflows.BoundJob) error
	JobPass          func(pass Pass, job workflows.BoundJob, visit func() error) error
	Step             func(workflows.BoundStep) error
	ParallelBoundary func()
}

// Visit traverses n depth-first, jobs in source order, and returns the first
// error a hook or pass reports.
func Visit(n workflows.BoundWorkflow, options Options) error {
	v := visitor{passes: options.Passes, hooks: options.Hooks, dbg: options.Debug}
	return v.visitWorkflow(n)
}

type visitor struct {
	passes []Pass
	hooks  Hooks
	dbg    io.Writer
}

func (v *visitor) reportElapsedTime(what string, start time.Time) {
	_, _ = fmt.Fprintf(v.dbg, "[Visitor] %s took %vms\n", what, time.Since(start).Milliseconds())
}

func (v *visitor) visitWorkflow(n workflows.BoundWorkflow) error {
	var t time.Time
	if v.dbg != nil {
		t = time.Now()
	}

	if v.hooks.Workflow != nil {
		if err := v.hooks.Workflow(n); err != nil {
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

	// Reusable metadata findings rely on source order to select the first visible caller.
	jobs := slices.SortedFunc(func(yield func(workflows.BoundJob) bool) {
		for _, job := range n.Jobs() {
			if !yield(job) {
				return
			}
		}
	}, func(a, b workflows.BoundJob) int {
		left, leftPresent := a.Position()
		right, rightPresent := b.Position()
		switch {
		case leftPresent && rightPresent:
			if order := cmp.Compare(left.Line, right.Line); order != 0 {
				return order
			}
			if order := cmp.Compare(left.Column, right.Column); order != 0 {
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

func (v *visitor) visitJob(n workflows.BoundJob) error {
	if v.hooks.Job != nil {
		if err := v.hooks.Job(n); err != nil {
			return err
		}
	}
	var t time.Time
	if v.dbg != nil {
		t = time.Now()
	}

	for _, p := range v.passes {
		if err := v.visitJobPass(p, n, func() error { return p.VisitJobPre(n) }); err != nil {
			return err
		}
	}

	if v.dbg != nil {
		v.reportElapsedTime(fmt.Sprintf("VisitWorkflowJobPre at job %q", n.ID.Text()), t)
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
		v.reportElapsedTime(fmt.Sprintf("Visiting %d steps at job %q", stepCount, n.ID.Text()), t)
		t = time.Now()
	}

	for _, p := range v.passes {
		if err := v.visitJobPass(p, n, func() error { return p.VisitJobPost(n) }); err != nil {
			return err
		}
	}

	if v.dbg != nil {
		v.reportElapsedTime(fmt.Sprintf("VisitWorkflowJobPost at job %q", n.ID.Text()), t)
	}

	return nil
}

func (v *visitor) visitJobPass(p Pass, n workflows.BoundJob, visit func() error) error {
	if v.hooks.JobPass != nil {
		return v.hooks.JobPass(p, n, visit)
	}
	return visit()
}

func (v *visitor) visitStep(n workflows.BoundStep) error {
	var t time.Time
	if v.dbg != nil {
		t = time.Now()
	}

	if v.hooks.Step != nil {
		if err := v.hooks.Step(n); err != nil {
			return err
		}
	} else {
		for _, p := range v.passes {
			if err := p.VisitStep(n); err != nil {
				return err
			}
		}
	}

	// Steps inside a 'parallel' step are visited too, and rules apply to them.
	// https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel/
	if steps, ok := n.ParallelSteps(); ok {
		for s := range steps {
			if v.hooks.ParallelBoundary != nil {
				v.hooks.ParallelBoundary()
			}
			if err := v.visitStep(s); err != nil {
				return err
			}
		}
		if v.hooks.ParallelBoundary != nil {
			v.hooks.ParallelBoundary()
		}
	}

	if v.dbg != nil {
		v.reportElapsedTime("VisitStep "+n.Reference().Node, t)
	}

	return nil
}
