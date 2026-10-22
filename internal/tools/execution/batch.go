package execution

import (
	"context"

	"actionlint.kjanat.dev/analyzer"

	repoconfig "actionlint.kjanat.dev/internal/config"
	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	workflowexecution "actionlint.kjanat.dev/internal/workflow/execution"
)

// Unit snapshots invocation context before analyzers run. Source mapping
// remains an adapter concern; backends receive text and explicit shell selection.
type Unit struct {
	Run       *workflowlang.ExecRun
	Input     *ActionInput
	Shell     *string
	Config    *repoconfig.Config
	Path      string
	Content   []byte
	Origin    *findingmodel.IgnoreOrigin
	Owner     *findingmodel.Owner
	Scope     *Scope
	Execution Execution
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
	Document   string
	Caller     string
	Call       *workflowlang.Step
	Depth      int
}

type Execution struct {
	Shell     workflowexecution.Shell
	Directory workflowexecution.Directory
}

type Batch struct {
	Scopes  []*Scope
	Scripts []Unit
}

type Analyzer interface {
	AnalyzeScripts(context.Context, Batch) ([]*findingmodel.Finding, error)
}
