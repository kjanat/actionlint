package engine

import "actionlint.kjanat.dev/internal/report"

type DocumentOutline = report.DocumentOutline
type DocumentOutlines = report.DocumentOutlines
type ActionRuns = report.ActionRuns
type CompositeRuns = report.CompositeRuns
type JavaScriptRuns = report.JavaScriptRuns
type DockerRuns = report.DockerRuns
type PluginRuns = report.PluginRuns
type UnknownRuns = report.UnknownRuns
type UsesReference = report.UsesReference
type RepositoryReference = report.RepositoryReference
type WorkspaceReference = report.WorkspaceReference
type SelfRepositoryReference = report.SelfRepositoryReference
type ContainerReference = report.ContainerReference
type BuiltinReference = report.BuiltinReference
type UnknownReference = report.UnknownReference
type WorkflowOutline = report.WorkflowOutline
type JobOutline = report.JobOutline
type StepOutline = report.StepOutline
type ActionOutline = report.ActionOutline
type ActionInputOutline = report.ActionInputOutline
type ActionOutputOutline = report.ActionOutputOutline

func ParseUsesReference(raw string) UsesReference { return report.ParseUsesReference(raw) }
func ParseWorkflowCallReference(raw string) UsesReference {
	return report.ParseWorkflowCallReference(raw)
}
