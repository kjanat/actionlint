package expressions

import (
	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"
	"actionlint.kjanat.dev/v2/internal/workflow/runner"
)

type typedExpr struct {
	ty  exprlang.Type
	pos workflowlang.Pos
}

// Checker validates expression syntax and types in workflow declarations.
// It checks syntax and semantics of the expressions including type checks and functions/contexts
// definitions. For more details see
// - https://docs.github.com/en/actions/learn-github-actions/contexts
// - https://docs.github.com/en/actions/learn-github-actions/expressions
type Checker struct {
	options          Options
	dependencies     Dependencies
	errs             []*findingmodel.Finding
	matrixTy         *exprlang.ObjectType
	matrixScriptSafe map[string]scriptValueProof
	scriptEnv        *workflowlang.Env
	jobEnv           *workflowlang.Env
	stepsTy          *exprlang.ObjectType
	needsTy          *exprlang.ObjectType
	secretsTy        *exprlang.ObjectType
	inputsTy         *exprlang.ObjectType
	dispatchInputsTy *exprlang.ObjectType
	jobsTy           *exprlang.ObjectType
	workflow         *workflowlang.Workflow
	eventTy          *exprlang.ObjectType
	expressionSource *workflowlang.ScriptSource
	expressionOffset int
	scriptScalarArgs map[int]bool
	scriptPosition   exprlang.ScriptPosition
	scriptJob        *workflowlang.Job
	compositeScript  bool
}

// Options selects expression diagnostics without repository configuration.
type Options struct {
	Semantics               exprlang.Options
	UnsoundTernary          bool
	ScriptInjectionDisabled bool
}

// Dependencies supplies metadata for workflow expression inference.
type Dependencies struct {
	SourceSnapshot *documentparse.SourceSnapshot
	Runners        *runner.Knowledge
	LocalAction    func(string) (*actionmeta.Metadata, error)
	PopularAction  func(string) *actionmeta.Metadata
	Workflow       func(string) (*calls.Metadata, error)
	Log            func(string, ...any)
}

// New creates an expression checker with independent per-workflow context state.
func New(dependencies Dependencies, options Options) *Checker {
	return &Checker{dependencies: dependencies, options: options}
}

// Configure replaces semantic options before checking a document.
func (rule *Checker) Configure(options Options) { rule.options = options }

// Findings returns diagnostics accumulated during workflow expression checking.
func (rule *Checker) Findings() []*findingmodel.Finding { return rule.errs }

func (rule *Checker) Error(pos *workflowlang.Pos, message string) {
	rule.errs = append(rule.errs, findingmodel.At(pos, "expression", message))
}

func (rule *Checker) Errorf(pos *workflowlang.Pos, format string, args ...any) {
	rule.errs = append(rule.errs, findingmodel.Errorf(pos, "expression", format, args...))
}

func (rule *Checker) debug(format string, args ...any) {
	if rule.dependencies.Log != nil {
		rule.dependencies.Log(format, args...)
	}
}

func (rule *Checker) actionMetadata(spec string) (*actionmeta.Metadata, error) {
	if rule.dependencies.LocalAction == nil {
		return nil, nil
	}
	return rule.dependencies.LocalAction(spec)
}

func (rule *Checker) workflowMetadata(spec string) (*calls.Metadata, error) {
	if rule.dependencies.Workflow == nil {
		return nil, nil
	}
	return rule.dependencies.Workflow(spec)
}

func (rule *Checker) popularAction(spec string) *actionmeta.Metadata {
	if rule.dependencies.PopularAction == nil {
		return nil
	}
	return rule.dependencies.PopularAction(spec)
}
