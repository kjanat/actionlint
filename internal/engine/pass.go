package engine

import (
	"cmp"
	"fmt"
	"io"
	"maps"
	"slices"
	"time"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
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
	Workflow         func(*workflowlang.Workflow)
	Job              func(*workflowlang.Job)
	Step             func(*workflowlang.Step) error
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
func (v *Visitor) Visit(n *workflowlang.Workflow) error {
	var t time.Time
	if v.dbg != nil {
		t = time.Now()
	}

	if v.Hooks.Workflow != nil {
		v.Hooks.Workflow(n)
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
	jobs := slices.SortedFunc(maps.Keys(n.Jobs), func(a, b string) int {
		left, right := n.Jobs[a].Pos, n.Jobs[b].Pos
		switch {
		case left != nil && right != nil:
			if order := cmp.Compare(left.Line, right.Line); order != 0 {
				return order
			}
			if order := cmp.Compare(left.Col, right.Col); order != 0 {
				return order
			}
		case left != nil:
			return -1
		case right != nil:
			return 1
		}
		return cmp.Compare(a, b)
	})
	for _, id := range jobs {
		if err := v.visitJob(n.Jobs[id]); err != nil {
			return err
		}
	}

	if v.dbg != nil {
		v.reportElapsedTime(fmt.Sprintf("Visiting %d jobs", len(n.Jobs)), t)
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

func (v *Visitor) visitJob(n *workflowlang.Job) error {
	if v.Hooks.Job != nil {
		v.Hooks.Job(n)
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
		v.reportElapsedTime(fmt.Sprintf("VisitWorkflowJobPre at job %q", n.ID.Value), t)
		t = time.Now()
	}

	for _, s := range n.Steps {
		if err := v.visitStep(s); err != nil {
			return err
		}
	}

	if v.dbg != nil {
		v.reportElapsedTime(fmt.Sprintf("Visiting %d steps at job %q", len(n.Steps), n.ID.Value), t)
		t = time.Now()
	}

	for _, p := range v.passes {
		if err := p.VisitJobPost(n); err != nil {
			return err
		}
	}

	if v.dbg != nil {
		v.reportElapsedTime(fmt.Sprintf("VisitWorkflowJobPost at job %q", n.ID.Value), t)
	}

	return nil
}

func (v *Visitor) visitStep(n *workflowlang.Step) error {
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
	if e, ok := n.Exec.(*workflowlang.ExecParallel); ok {
		for _, s := range e.Steps {
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
		v.reportElapsedTime(fmt.Sprintf("VisitStep at %s", n.Pos), t)
	}

	return nil
}
