package execution

import (
	"context"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowexecution "actionlint.kjanat.dev/v2/internal/workflow/execution"
)

// Unit snapshots invocation context before analyzers run. Source mapping
// remains an adapter concern; backends receive text and explicit shell selection.
type Unit struct {
	Run       *workflowlang.ExecRun
	Shell     *string
	Config    *repoconfig.Config
	Path      string
	Content   []byte
	Origin    *findingmodel.IgnoreOrigin
	Owner     *findingmodel.Owner
	Scope     *Scope
	Execution Execution
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
