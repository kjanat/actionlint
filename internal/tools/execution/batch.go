package execution

import (
	"context"
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/analyzer"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/invocationview"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowexecution "actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	"actionlint.kjanat.dev/v2/source"
)

// Unit snapshots invocation context before analyzers run.
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

// ScriptRequest contains invocation policy; declarations come from the source registry.
type ScriptRequest struct {
	Occurrence   invocation.ID
	DefaultShell *string
	Input        *analyzer.ScriptInput
	Execution    Execution
}

type Collection struct {
	Declarations *documentparse.SourceSnapshots
	Scopes       []*Scope
	Scripts      []ScriptRequest
	Collected    bool
}

// Scope identifies a workflow or composite invocation independently of
// whether it contains scripts, so configuration failures are still observable.
type Scope struct {
	Paths      workflowexecution.Paths
	Config     *repoconfig.Config
	Deployment Deployment
	Composite  bool
	Context    invocation.Context
}

type Execution struct {
	Shell     workflowexecution.Shell
	Directory workflowexecution.Directory
}

var ErrInvalidBatch = errors.New("execution: invalid prepared batch")

// Batch binds execution inputs to a published graph. Its zero value is unprepared.
type Batch struct{ data *batchData }

type batchData struct {
	graph      invocation.Graph
	projection invocationview.Projection
	scopes     []*Scope
	scripts    []Unit
	collected  bool
}

// NewBatch validates complete script collection or an explicit scopes-only batch.
func NewBatch(graph invocation.Graph, collection Collection) (Batch, error) {
	scopes, requests, collected := collection.Scopes, collection.Scripts, collection.Collected
	if !graph.Has(graph.RootID()) {
		return Batch{}, fmt.Errorf("%w: graph was not published", ErrInvalidBatch)
	}
	if collected && len(requests) != len(graph.Scripts()) || !collected && len(requests) != 0 {
		return Batch{}, fmt.Errorf("%w: script collection disagrees with registered occurrences", ErrInvalidBatch)
	}
	owners := make(map[invocation.ID]*Scope, len(scopes))
	for _, scope := range scopes {
		if scope == nil || owners[scope.Context.ID()] != nil {
			return Batch{}, fmt.Errorf("%w: missing or duplicate scope", ErrInvalidBatch)
		}
		if err := validateScope(scope, graph); err != nil {
			return Batch{}, err
		}
		owners[scope.Context.ID()] = scope
	}
	_, interrupted := graph.EntryInterruption()
	_, stopped := graph.Stop()
	if owners[graph.RootID()] == nil && !interrupted && !stopped {
		return Batch{}, fmt.Errorf("%w: entry scope is missing", ErrInvalidBatch)
	}
	for _, call := range graph.Calls() {
		if call.Outcome.Kind() == invocation.OutcomeExpanded && owners[call.ID()] == nil {
			return Batch{}, fmt.Errorf("%w: completed invocation scope %q is missing", ErrInvalidBatch, call.ID())
		}
	}
	scopes, _ = cloneInputs(scopes, nil)
	for _, scope := range scopes {
		owners[scope.Context.ID()] = scope
	}
	scripts, err := prepareScripts(graph, collection, owners)
	if err != nil {
		return Batch{}, err
	}
	projection, err := invocationview.New(graph)
	if err != nil {
		return Batch{}, fmt.Errorf("%w: %w", ErrInvalidBatch, err)
	}
	return Batch{&batchData{graph: graph, projection: projection, scopes: scopes, scripts: scripts, collected: collected}}, nil
}

func validateScope(scope *Scope, graph invocation.Graph) error {
	if !graph.Owns(scope.Context) {
		return fmt.Errorf("%w: foreign invocation context", ErrInvalidBatch)
	}
	paths := scope.Paths
	workflowexecution.ActionOrigin(&paths, scope.Context, scope.Deployment.ActionPath())
	if paths.ActionPath != scope.Paths.ActionPath || paths.ActionRunnerPath != scope.Paths.ActionRunnerPath || paths.ActionIndependent != scope.Paths.ActionIndependent {
		return fmt.Errorf("%w: action paths disagree with invocation context", ErrInvalidBatch)
	}
	if scope.Context.IsEntry() {
		if graph.Entry().Kind == invocation.EntryWorkflow && scope.Deployment != (Deployment{}) {
			return fmt.Errorf("%w: workflow entry cannot have a deployment", ErrInvalidBatch)
		}
		if scope.Composite && graph.Entry().Kind != invocation.EntryAction {
			return fmt.Errorf("%w: workflow entry cannot be composite", ErrInvalidBatch)
		}
		if graph.Entry().Kind == invocation.EntryAction && scope.Deployment.reference != graph.Entry().Reference {
			return fmt.Errorf("%w: action entry deployment disagrees with source", ErrInvalidBatch)
		}
		return nil
	}
	call, exists := graph.Call(scope.Context.ID())
	target, hasBody := call.Outcome.Target()
	kind := call.Outcome.Kind()
	if !exists || !hasBody {
		return fmt.Errorf("%w: call scope has no resolved body", ErrInvalidBatch)
	}
	if kind != invocation.OutcomeExpanded && kind != invocation.OutcomeInterrupted {
		return fmt.Errorf("%w: call scope outcome is not executable", ErrInvalidBatch)
	}
	if !scope.Composite {
		return fmt.Errorf("%w: call scope must be composite", ErrInvalidBatch)
	}
	if scope.Deployment.reference != target {
		return fmt.Errorf("%w: call deployment disagrees with source", ErrInvalidBatch)
	}
	return nil
}

func (batch Batch) Valid() bool { return batch.data != nil }

func (batch Batch) CallGraph() analyzer.CallGraph {
	if batch.data == nil {
		return analyzer.CallGraph{}
	}
	return batch.data.projection.Graph()
}

func (batch Batch) Invocations() invocation.Graph {
	if batch.data == nil {
		return invocation.Graph{}
	}
	return batch.data.graph
}

func (batch Batch) CollectedScripts() bool { return batch.data != nil && batch.data.collected }

// Snapshot copies mutable source values, preserving scope links within the copy.
func (batch Batch) Snapshot() ([]*Scope, []Unit) {
	if batch.data == nil {
		return nil, nil
	}
	return cloneInputs(batch.data.scopes, batch.data.scripts)
}

func (batch Batch) Scripts() []Unit  { _, scripts := batch.Snapshot(); return scripts }
func (batch Batch) Scopes() []*Scope { scopes, _ := batch.Snapshot(); return scopes }

type Analyzer interface {
	AnalyzeScripts(context.Context, Batch) ([]*findingmodel.Finding, error)
}
