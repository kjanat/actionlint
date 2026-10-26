package execution

import (
	"context"

	"actionlint.kjanat.dev/v2/analyzer"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowexecution "actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	"actionlint.kjanat.dev/v2/source"
)

// Unit snapshots invocation context before analyzers run. Source mapping
// remains an adapter concern; backends receive text and explicit shell selection.
type Unit struct {
	Occurrence invocation.ID
	Reference  source.Reference
	Run        *workflowlang.ExecRun
	Input      *ActionInput
	Shell      *string
	Config     *repoconfig.Config
	Path       string
	Content    []byte
	Origin     *findingmodel.IgnoreOrigin
	Owner      *findingmodel.Owner
	Scope      *Scope
	Execution  Execution
}

// ActionInput is a typed, mapped action argument, never a shell run command.
type ActionInput struct {
	Site  analyzer.ScriptInput
	Value *workflowlang.Input
}

// Scope identifies a workflow or composite invocation independently of
// whether it contains scripts, so configuration failures are still observable.
type Scope struct {
	Paths      workflowexecution.Paths
	Config     *repoconfig.Config
	ActionPath string
	Composite  bool
	Occurrence invocation.ID
	Context    workflowexecution.ActionContext
}

type Execution struct {
	Shell     workflowexecution.Shell
	Directory workflowexecution.Directory
}

type Batch struct {
	Invocations invocation.Graph
	Scopes      []*Scope
	Scripts     []Unit
}

type Analyzer interface {
	AnalyzeScripts(context.Context, Batch) ([]*findingmodel.Finding, error)
}
