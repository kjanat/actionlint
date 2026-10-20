// Package document defines report projections and structured action references.
// These declarations are independent of parsing, discovery and analyzer execution.
package document

import "actionlint.kjanat.dev/internal/report"

type Outline = report.DocumentOutline
type Outlines = report.DocumentOutlines
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
type Workflow = report.WorkflowOutline
type Job = report.JobOutline
type Step = report.StepOutline
type Action = report.ActionOutline
type Input = report.ActionInputOutline
type Output = report.ActionOutputOutline

// ParseReference classifies a declared action source without fetching its target.
func ParseReference(raw string) UsesReference { return report.ParseUsesReference(raw) }

// ParseWorkflowReference classifies a reusable workflow source.
func ParseWorkflowReference(raw string) UsesReference { return report.ParseWorkflowCallReference(raw) }
