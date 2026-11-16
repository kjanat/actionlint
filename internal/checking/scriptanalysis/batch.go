package scriptanalysis

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/configuration/rulebinding"
	"actionlint.kjanat.dev/v2/internal/model/commands"
	"actionlint.kjanat.dev/v2/internal/model/workflows"

	"actionlint.kjanat.dev/v2/internal/model/checkinputs"

	"actionlint.kjanat.dev/v2/internal/base/files"
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/scripts"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

// Unit snapshots invocation context before analyzers run.
type Unit struct {
	Revision    sourcetext.Revision
	Coordinates sourcetext.Coordinates
	Occurrence  invocation.ID
	Reference   sourcetext.Reference
	Run         *definition.ExecRun
	Input       *ActionInput
	Shell       *string
	Path        string
	Content     []byte
	Origin      *workflows.IgnoreOrigin
	Owner       *finding.Owner
	Scope       *Scope
	// Execution is nil when the analysis did not resolve the run's shell and
	// working directory.
	Execution    *scripts.Execution
	Environment  []scripts.EnvironmentNames
	Interpreters *commands.Registry
}

// ActionInput is a typed, mapped action argument, never a shell run command.
type ActionInput struct {
	Site  checkinputs.ScriptInput
	Value *definition.ActionArgument
}

// Text returns the script text: the action input value or the run body.
func (u Unit) Text() string {
	if u.Input != nil {
		return u.Input.Value.Value.Text()
	}
	if u.Run == nil {
		return ""
	}
	return u.Run.Run.Text()
}

// Source maps Text back to its declaring YAML scalar.
func (u Unit) Source() *definition.ScriptSource {
	if u.Input != nil {
		return u.Input.Value.Source
	}
	if u.Run == nil {
		return nil
	}
	return u.Run.Source
}

// Key is the position of the input name or the run key.
func (u Unit) Key() *sourcetext.Position {
	if u.Input != nil {
		return u.Input.Value.Name.Pos
	}
	if u.Run == nil {
		return nil
	}
	return u.Run.RunPos
}

// Value is the position of the script value.
func (u Unit) Value() *sourcetext.Position {
	if u.Input != nil {
		return u.Input.Value.Value.Pos
	}
	if u.Run == nil || u.Run.Run == nil {
		return nil
	}
	return u.Run.Run.Pos
}

// Config is the configuration of the script's scope.
func (u Unit) Config() *configuration.Config {
	if u.Scope == nil {
		return nil
	}
	return u.Scope.Config
}

// RuntimeShell returns the resolved shell. Unresolved execution returns the
// zero shell.
func (u Unit) RuntimeShell() scripts.Shell {
	if u.Execution == nil {
		return scripts.Shell{}
	}
	return u.Execution.Shell
}

// WorkingDirectory returns the directory a tool runs the script in and whether
// it is known. Unresolved execution keeps the tool's own working directory, so
// it reports an empty, known directory.
func (u Unit) WorkingDirectory() (string, bool) {
	if u.Execution == nil {
		return "", true
	}
	return u.Execution.Directory, u.Execution.DirectoryKnown
}

// ScriptRequest contains invocation policy; declarations come from the source registry.
type ScriptRequest struct {
	Occurrence   invocation.ID
	DefaultShell *string
	Input        *checkinputs.ScriptInput
	Execution    *scripts.Execution
	Environment  []scripts.EnvironmentNames
	Interpreters *commands.Registry
}

type Collection struct {
	Declarations *parsing.SourceSnapshots
	Scopes       []*Scope
	Scripts      []ScriptRequest
	Collected    bool
}

// Scope identifies a workflow or composite invocation independently of
// whether it contains scripts, so configuration failures are still observable.
type Scope struct {
	Bindings *rulebinding.Scope
	Paths    execution.Paths
	Reader   *files.Reader // records the filesystem reads tools make in this scope
	// Config is a host-owned prepared value. Internal consumers treat it as read-only;
	// ForTool detaches selected settings before a tool reads them.
	Config           *configuration.Config
	ConfigProvenance configuration.Provenance
	Deployment       Deployment
	Composite        bool
	Context          invocation.Context
}

var ErrInvalidBatch = errors.New("execution: invalid prepared batch")

// Batch binds execution inputs to a published graph. NewBatch validates it;
// its zero value is unprepared. Tools receive the batch ForTool returns and
// may select subsets of its scripts and scopes.
type Batch struct {
	CallGraph invocation.Graph
	Scopes    []*Scope
	Scripts   []Unit
	// Collected reports complete script collection. A scopes-only batch has none.
	Collected bool
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
	scripts, err := prepareScripts(graph, collection, owners)
	if err != nil {
		return Batch{}, err
	}
	return Batch{CallGraph: graph, Scopes: scopes, Scripts: scripts, Collected: collected}, nil
}

func validateScope(scope *Scope, graph invocation.Graph) error {
	if !graph.Owns(scope.Context) {
		return fmt.Errorf("%w: foreign invocation context", ErrInvalidBatch)
	}
	paths := scope.Paths
	execution.ActionOrigin(&paths, scope.Context, scope.Deployment.ActionPath())
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

func (batch Batch) Valid() bool { return batch.CallGraph.Valid() }

// ForTool returns the batch the named tool analyzes. Each scope carries a
// detached copy of the configuration bound to the tool, and each script its own
// environment, so a tool cannot change what other analyzers read.
func (batch Batch) ForTool(name string) (Batch, error) {
	view := Batch{CallGraph: batch.CallGraph, Collected: batch.Collected, Scopes: make([]*Scope, 0, len(batch.Scopes)), Scripts: slices.Clone(batch.Scripts)}
	scopes := make(map[*Scope]*Scope, len(batch.Scopes))
	for _, scope := range batch.Scopes {
		config := scope.Config
		if scope.Bindings != nil {
			config = scope.Bindings.Rule(name).Config()
		}
		detached, err := config.Detached()
		if err != nil {
			return Batch{}, fmt.Errorf("prepare tool %q configuration: %w", name, err)
		}
		owned := *scope
		owned.Config = detached
		scopes[scope] = &owned
		view.Scopes = append(view.Scopes, &owned)
	}
	for i := range view.Scripts {
		view.Scripts[i].Scope = scopes[view.Scripts[i].Scope]
		view.Scripts[i].Environment = slices.Clone(view.Scripts[i].Environment)
	}
	return view, nil
}

type BatchAnalyzer interface {
	AnalyzeScripts(context.Context, Batch) ([]*finding.Finding, error)
}
