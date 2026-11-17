package outline

import (
	"cmp"
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

// FromWorkflow projects decoded workflow declarations into a report outline.
func FromWorkflow(path string, workflow *definition.Workflow, parseErrors bool) WorkflowOutline {
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
	jobs := make([]JobOutline, 0, len(workflow.Jobs))
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
		} else {
			record.Uses = outlineString(job.DeclaredUses)
		}
		jobs = append(jobs, record)
	}
	slices.SortFunc(jobs, func(a, b JobOutline) int {
		switch {
		case a.Start != nil && b.Start != nil:
			if order := cmp.Compare(a.Start.Line, b.Start.Line); order != 0 {
				return order
			}
			if order := cmp.Compare(a.Start.Column, b.Start.Column); order != 0 {
				return order
			}
		case a.Start != nil:
			return -1
		case b.Start != nil:
			return 1
		}
		return cmp.Compare(a.ID, b.ID)
	})
	outline.Jobs = append(outline.Jobs, jobs...)
	return outline
}

func stepOutlines(steps []*definition.Step) []StepOutline {
	records := make([]StepOutline, 0, len(steps))
	for _, step := range steps {
		record := StepOutline{
			ID: outlineString(step.ID), Name: outlineString(step.Name),
			Start: outlinePosition(step.Pos), Kind: unknownStepKind, Uses: outlineString(step.DeclaredUses),
		}
		if step.Exec != nil {
			record.Kind = step.Exec.Kind().String()
		}
		switch exec := step.Exec.(type) {
		case *definition.ExecAction:
			record.Uses = outlineString(exec.Uses)
		case *definition.ExecParallel:
			record.Steps = stepOutlines(exec.Steps)
		}
		records = append(records, record)
	}
	return records
}

func outlineString(value *definition.String) string {
	if value == nil {
		return ""
	}
	return value.Value
}

func outlinePosition(pos *sourcetext.Position) *sourcetext.Position {
	if pos == nil {
		return nil
	}
	clone := *pos
	return &clone
}
