package actionlint

import (
	"cmp"
	"slices"
)

// WorkflowOutline describes declared workflow structure, not runtime execution.
// ParseStatus is complete, partial (a tree with parser errors), or failed (no tree).
// Parsing can complete even when analysis fails or is canceled.
type WorkflowOutline struct {
	Path        string       `json:"path"`
	Name        string       `json:"name,omitempty"`
	ParseStatus string       `json:"parse_status"`
	Triggers    []string     `json:"triggers"`
	Jobs        []JobOutline `json:"jobs"`
}

// JobOutline retains declared IDs and references, including unresolved expressions.
// Jobs appear in source order. Start points to the job key, when available.
type JobOutline struct {
	ID    string              `json:"id"`
	Name  string              `json:"name,omitempty"`
	Start *DiagnosticPosition `json:"start,omitempty"`
	Needs []string            `json:"needs"`
	Uses  string              `json:"uses,omitempty"`
	Steps []StepOutline       `json:"steps"`
}

// StepOutline identifies a declared step without exporting scripts or parser internals.
// Kind is run, uses, wait, cancel, parallel, or unknown; consumers must allow new kinds.
// Steps contains the children of a parallel group, in declaration order.
type StepOutline struct {
	ID    string              `json:"id,omitempty"`
	Name  string              `json:"name,omitempty"`
	Start *DiagnosticPosition `json:"start,omitempty"`
	Kind  string              `json:"kind"`
	Uses  string              `json:"uses,omitempty"`
	Steps []StepOutline       `json:"steps,omitempty"`
}

func workflowOutline(path string, workflow *Workflow, parseErrors bool) WorkflowOutline {
	outline := WorkflowOutline{Path: path, ParseStatus: "failed", Triggers: []string{}, Jobs: []JobOutline{}}
	if workflow == nil {
		return outline
	}
	outline.ParseStatus = "complete"
	if parseErrors {
		outline.ParseStatus = "partial"
	}
	outline.Name = outlineString(workflow.Name)
	for _, event := range workflow.On {
		outline.Triggers = append(outline.Triggers, event.EventName())
	}
	for _, job := range workflow.Jobs {
		record := JobOutline{
			ID: outlineString(job.ID), Name: outlineString(job.Name), Start: outlinePosition(job.Pos),
			Needs: []string{}, Steps: stepOutlines(job.Steps),
		}
		for _, need := range job.Needs {
			record.Needs = append(record.Needs, need.Value)
		}
		if job.WorkflowCall != nil {
			record.Uses = outlineString(job.WorkflowCall.Uses)
		}
		outline.Jobs = append(outline.Jobs, record)
	}
	slices.SortFunc(outline.Jobs, func(a, b JobOutline) int {
		if a.Start != nil && b.Start != nil {
			if order := cmp.Compare(a.Start.Line, b.Start.Line); order != 0 {
				return order
			}
			if order := cmp.Compare(a.Start.Column, b.Start.Column); order != 0 {
				return order
			}
		} else if a.Start != nil {
			return -1
		} else if b.Start != nil {
			return 1
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return outline
}

func stepOutlines(steps []*Step) []StepOutline {
	records := make([]StepOutline, 0, len(steps))
	for _, step := range steps {
		record := StepOutline{
			ID: outlineString(step.ID), Name: outlineString(step.Name),
			Start: outlinePosition(step.Pos), Kind: "unknown",
		}
		switch exec := step.Exec.(type) {
		case *ExecRun:
			record.Kind = "run"
		case *ExecAction:
			record.Kind, record.Uses = "uses", outlineString(exec.Uses)
		case *ExecWait:
			record.Kind = "wait"
		case *ExecCancel:
			record.Kind = "cancel"
		case *ExecParallel:
			record.Kind, record.Steps = "parallel", stepOutlines(exec.Steps)
		}
		records = append(records, record)
	}
	return records
}

func outlineString(value *String) string {
	if value == nil {
		return ""
	}
	return value.Value
}

func outlinePosition(pos *Pos) *DiagnosticPosition {
	if pos == nil {
		return nil
	}
	return &DiagnosticPosition{Line: pos.Line, Column: pos.Col}
}
