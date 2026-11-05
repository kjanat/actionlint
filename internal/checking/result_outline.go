package checking

import (
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/results/outline"
)

func documentOutline(path string, document parsing.Document) outline.Outline {
	switch document.Kind() {
	case parsing.WorkflowKind:
		return outline.FromWorkflow(path, document.Workflow, len(document.SyntaxErrors) != 0)
	case parsing.ActionKind:
		return outline.FromAction(path, document.Metadata, document.ParseError)
	default:
		return nil
	}
}
