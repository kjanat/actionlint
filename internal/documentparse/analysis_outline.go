package documentparse

import (
	"cmp"
	"slices"

	documentmodel "actionlint.kjanat.dev/document"

	diagmodel "actionlint.kjanat.dev/diagnostic"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func WorkflowOutline(path string, workflow *workflowlang.Workflow, parseErrors bool) documentmodel.Workflow {
	outline := documentmodel.Workflow{Path: path, ParseStatus: "failed", Triggers: []string{}, Jobs: []documentmodel.Job{}}
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
		record := documentmodel.Job{
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
		record.Reference = documentmodel.ParseWorkflowReference(record.Uses)
		outline.Jobs = append(outline.Jobs, record)
	}
	slices.SortFunc(outline.Jobs, func(a, b documentmodel.Job) int {
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
	return outline
}

func stepOutlines(steps []*workflowlang.Step) []documentmodel.Step {
	records := make([]documentmodel.Step, 0, len(steps))
	for _, step := range steps {
		record := documentmodel.Step{
			ID: outlineString(step.ID), Name: outlineString(step.Name),
			Start: outlinePosition(step.Pos), Kind: "unknown", Uses: outlineString(step.DeclaredUses),
		}
		switch exec := step.Exec.(type) {
		case *workflowlang.ExecRun:
			record.Kind = "run"
		case *workflowlang.ExecAction:
			record.Kind, record.Uses = "uses", outlineString(exec.Uses)
		case *workflowlang.ExecWait:
			record.Kind = "wait"
		case *workflowlang.ExecCancel:
			record.Kind = "cancel"
		case *workflowlang.ExecParallel:
			record.Kind, record.Steps = "parallel", stepOutlines(exec.Steps)
		}
		record.Reference = documentmodel.ParseReference(record.Uses)
		records = append(records, record)
	}
	return records
}

func outlineString(value *workflowlang.String) string {
	if value == nil {
		return ""
	}
	return value.Value
}

func outlinePosition(pos *workflowlang.Pos) *diagmodel.Position {
	if pos == nil {
		return nil
	}
	return &diagmodel.Position{Line: pos.Line, Column: pos.Col}
}
