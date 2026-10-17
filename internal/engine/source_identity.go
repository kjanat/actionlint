package engine

import (
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	workflowfacts "actionlint.kjanat.dev/internal/workflow/facts"
)

type sourceReferences = workflowfacts.References

func workflowReferences(path string, content []byte, workflow *workflowlang.Workflow, action bool) sourceReferences {
	return workflowfacts.Index(path, content, workflow, action)
}
