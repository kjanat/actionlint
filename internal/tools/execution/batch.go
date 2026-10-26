package execution

import (
	"context"
	"errors"
	"fmt"

	"actionlint.kjanat.dev/v2/analyzer"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
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

// Scope identifies a workflow or composite invocation independently of
// whether it contains scripts, so configuration failures are still observable.
type Scope struct {
	Paths      workflowexecution.Paths
	Config     *repoconfig.Config
	ActionPath string
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
	graph     invocation.Graph
	scopes    []*Scope
	scripts   []Unit
	collected bool
}

// NewBatch validates complete script collection or an explicit scopes-only batch.
func NewBatch(graph invocation.Graph, scopes []*Scope, scripts []Unit, collected bool) (Batch, error) {
	if !graph.Has(graph.RootID()) {
		return Batch{}, fmt.Errorf("%w: graph was not published", ErrInvalidBatch)
	}
	if collected && len(scripts) != len(graph.Scripts()) || !collected && len(scripts) != 0 {
		return Batch{}, fmt.Errorf("%w: script collection disagrees with registered occurrences", ErrInvalidBatch)
	}
	knownScopes := make(map[*Scope]bool, len(scopes))
	owners := make(map[invocation.ID]bool, len(scopes))
	for _, scope := range scopes {
		if scope == nil || owners[scope.Context.ID()] {
			return Batch{}, fmt.Errorf("%w: missing or duplicate scope", ErrInvalidBatch)
		}
		if err := validateScope(scope, graph); err != nil {
			return Batch{}, err
		}
		knownScopes[scope], owners[scope.Context.ID()] = true, true
	}
	_, interrupted := graph.EntryInterruption()
	_, stopped := graph.Stop()
	if !owners[graph.RootID()] && !interrupted && !stopped {
		return Batch{}, fmt.Errorf("%w: entry scope is missing", ErrInvalidBatch)
	}
	for _, call := range graph.Calls() {
		if call.Outcome.Kind() == invocation.OutcomeExpanded && !owners[call.ID()] {
			return Batch{}, fmt.Errorf("%w: completed invocation scope %q is missing", ErrInvalidBatch, call.ID())
		}
	}
	seen := make(map[invocation.ID]bool, len(scripts))
	for _, unit := range scripts {
		record, exists := graph.Script(unit.Occurrence)
		if !exists || seen[unit.Occurrence] || record.Declaration != unit.Reference {
			return Batch{}, fmt.Errorf("%w: unregistered, duplicated or mismatched script %q", ErrInvalidBatch, unit.Occurrence)
		}
		if !knownScopes[unit.Scope] || record.Owner != unit.Scope.Context.ID() || unit.Config != unit.Scope.Config {
			return Batch{}, fmt.Errorf("%w: script %q has a foreign scope or configuration", ErrInvalidBatch, unit.Occurrence)
		}
		switch record.Kind {
		case invocation.ScriptRun:
			if unit.Run == nil || unit.Run.Run == nil || unit.Run.Source == nil || unit.Run.Run.Pos == nil || unit.Run.RunPos == nil || unit.Input != nil {
				return Batch{}, fmt.Errorf("%w: run occurrence %q needs one run body", ErrInvalidBatch, unit.Occurrence)
			}
		case invocation.ScriptActionInput:
			input := unit.Input
			if unit.Run != nil || input == nil || input.Value == nil || input.Value.Name == nil || input.Value.Name.Pos == nil || input.Value.Value == nil || input.Value.Value.Pos == nil || input.Value.Source == nil || input.Site.Input != record.Input {
				return Batch{}, fmt.Errorf("%w: input occurrence %q needs its registered argument", ErrInvalidBatch, unit.Occurrence)
			}
			if err := analyzer.ValidateScriptInput(input.Site); err != nil {
				return Batch{}, fmt.Errorf("%w: %w", ErrInvalidBatch, err)
			}
		default:
			return Batch{}, fmt.Errorf("%w: unknown script kind", ErrInvalidBatch)
		}
		seen[unit.Occurrence] = true
	}
	scopes, scripts = cloneInputs(scopes, scripts)
	return Batch{&batchData{graph: graph, scopes: scopes, scripts: scripts, collected: collected}}, nil
}

func validateScope(scope *Scope, graph invocation.Graph) error {
	if !graph.Owns(scope.Context) {
		return fmt.Errorf("%w: foreign invocation context", ErrInvalidBatch)
	}
	paths := scope.Paths
	workflowexecution.ActionOrigin(&paths, scope.Context, scope.ActionPath)
	if paths.ActionPath != scope.Paths.ActionPath || paths.ActionRunnerPath != scope.Paths.ActionRunnerPath || paths.ActionIndependent != scope.Paths.ActionIndependent {
		return fmt.Errorf("%w: action paths disagree with invocation context", ErrInvalidBatch)
	}
	if scope.Context.IsEntry() {
		if scope.Composite && graph.Entry().Kind != invocation.EntryAction {
			return fmt.Errorf("%w: workflow entry cannot be composite", ErrInvalidBatch)
		}
		return nil
	}
	call, exists := graph.Call(scope.Context.ID())
	_, hasBody := call.Outcome.Target()
	kind := call.Outcome.Kind()
	if !exists || !hasBody || kind != invocation.OutcomeExpanded && kind != invocation.OutcomeInterrupted || !scope.Composite {
		return fmt.Errorf("%w: scope does not describe an expanded invocation", ErrInvalidBatch)
	}
	return nil
}

func (batch Batch) Valid() bool { return batch.data != nil }

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
