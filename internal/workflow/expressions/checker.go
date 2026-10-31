package expressions

import (
	"fmt"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
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
	options      Options
	dependencies Dependencies
	errs         []report
	// The *Ty context types are nil while the context keeps its built-in type.
	matrixTy         *exprlang.ObjectType
	matrixScriptSafe map[string]scriptValueProof
	scriptEnv        workflowdecl.EnvView
	jobEnv           workflowdecl.EnvView
	stepsTy          *exprlang.ObjectType
	needsTy          *exprlang.ObjectType
	secretsTy        *exprlang.ObjectType
	inputsTy         *exprlang.ObjectType
	dispatchInputsTy *exprlang.ObjectType
	jobsTy           *exprlang.ObjectType
	workflow         workflowdecl.WorkflowView
	eventTy          *exprlang.ObjectType
	expressionSource workflowdecl.ScriptSourceView
	scriptSources    map[workflowlang.Pos]workflowdecl.ScriptSourceView
	expressionOffset int
	scriptScalarArgs map[int]bool
	scriptPosition   exprlang.ScriptPosition
	scriptJob        workflowdecl.JobView
	compositeScript  bool
	jobReferences    *ReferenceNames
}

// ReferenceNames contains complete name sets; nil leaves membership unknown.
type ReferenceNames struct {
	Variables, Secrets []string
	EnvironmentSecrets *exprlang.ObjectType
}

// Options selects expression diagnostics without repository configuration.
type Options struct {
	Semantics               exprlang.Options
	UnsoundTernary          bool
	ScriptInjectionDisabled bool
	JobReferences           func(workflowdecl.JobView) ReferenceNames
}

// Dependencies supplies metadata for workflow expression inference.
type Dependencies struct {
	Runners      *runner.Knowledge
	LocalAction  func(string) (*actionmeta.Metadata, error)
	RemoteAction func(string) (actionmeta.Definition, error)
	Workflow     func(string) (*calls.Metadata, error)
	Log          func(string, ...any)
}

// New creates an expression checker with independent per-workflow context state.
func New(dependencies Dependencies, options Options) *Checker {
	return &Checker{dependencies: dependencies, options: options}
}

// Configure replaces semantic options before checking a document.
func (rule *Checker) Configure(options Options) { rule.options = options }

// report is the stored form of an expression finding. Expression findings only
// carry a rule, code, message and source range, so they are held as plain
// values; every reader receives fresh findings and cannot alter the checker's.
type report struct {
	rule, code, message string
	start, end          workflowlang.Pos
	// hasEnd records whether the expression mapped an inclusive end position.
	hasEnd bool
}

func (r report) finding() *findingmodel.Finding {
	finding := findingmodel.At(&r.start, r.rule, r.message)
	finding.Code = r.code
	if r.hasEnd {
		end := r.end
		finding.EndPosition = &end
	}
	return finding
}

// Findings returns diagnostics accumulated during workflow expression checking.
// The result is detached from the checker.
func (rule *Checker) Findings() []*findingmodel.Finding { return rule.FindingsSince(0) }

// FindingCount returns the number of diagnostics accumulated so far. Pass it to
// FindingsSince to read only the diagnostics reported after this point.
func (rule *Checker) FindingCount() int { return len(rule.errs) }

// FindingsSince returns diagnostics reported after mark, a prior FindingCount.
// The result is detached from the checker.
func (rule *Checker) FindingsSince(mark int) []*findingmodel.Finding {
	if mark >= len(rule.errs) {
		return nil
	}
	findings := make([]*findingmodel.Finding, 0, len(rule.errs)-mark)
	for _, r := range rule.errs[mark:] {
		findings = append(findings, r.finding())
	}
	return findings
}

func (rule *Checker) Error(pos *workflowlang.Pos, message string) {
	rule.errs = append(rule.errs, report{rule: "expression", message: message, start: *pos})
}

func (rule *Checker) Errorf(pos *workflowlang.Pos, format string, args ...any) {
	rule.Error(pos, fmt.Sprintf(format, args...))
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

func (rule *Checker) remoteAction(spec string) (actionmeta.Definition, error) {
	if rule.dependencies.RemoteAction == nil {
		return actionmeta.Definition{}, nil
	}
	return rule.dependencies.RemoteAction(spec)
}
