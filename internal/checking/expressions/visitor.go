package expressions

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"

	"actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"

	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/calls"

	"actionlint.kjanat.dev/v2/internal/model/runners"

	"actionlint.kjanat.dev/v2/internal/model/workflows"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/results"

	"actionlint.kjanat.dev/v2/internal/model/severity"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

type typedExpr struct {
	ty         exprtypes.Type
	semantics  typecheck.ExpressionCheckResult
	pos        sourcetext.Position
	refinement expressionRefinement
}

// Visitor validates expression syntax and types in workflow declarations.
// It checks syntax and semantics of the expressions including type checks and functions/contexts
// definitions. For more details see
// - https://docs.github.com/en/actions/learn-github-actions/contexts
// - https://docs.github.com/en/actions/learn-github-actions/expressions
type Visitor struct {
	options      Options
	dependencies Dependencies
	errs         []report
	// The *Ty context types are nil while the context keeps its built-in type.
	matrixTy          *exprtypes.ObjectType
	matrixScriptSafe  map[string]scriptValueProof
	scriptEnv         *definition.Env
	jobEnv            *definition.Env
	stepsTy           *exprtypes.ObjectType
	needsTy           *exprtypes.ObjectType
	secretsTy         *exprtypes.ObjectType
	inputsTy          *exprtypes.ObjectType
	dispatchInputsTy  *exprtypes.ObjectType
	jobsTy            *exprtypes.ObjectType
	workflow          workflows.BoundWorkflow
	eventTy           *exprtypes.ObjectType
	expressionSource  *definition.ScriptSource
	scriptSources     map[sourcetext.Position]*definition.ScriptSource
	expressionOffset  int
	scriptScalarArgs  map[int]bool
	scriptWordLists   map[int]bool
	scriptEnvironment map[int]scriptEnvironmentReplacement
	scriptInsertion   correctness.ScriptInsertionKind
	scriptJob         workflows.BoundJob
	compositeScript   bool
	jobReferences     *ReferenceNames
}

// ReferenceNames contains complete name sets; nil leaves membership unknown.
type ReferenceNames struct {
	Variables, Secrets []string
	EnvironmentSecrets *exprtypes.ObjectType
}

// Options selects expression diagnostics without repository configuration.
type Options struct {
	Semantics                 typecheck.Options
	UnsoundTernary            bool
	SuspiciousCoercion        bool
	JoinLiteral               bool
	CollectionComparison      bool
	StringConditions          bool
	MixedTypeComparisons      bool
	CaseInsensitiveConditions bool
	ScriptInjectionDisabled   bool
	JobReferences             func(workflows.BoundJob) ReferenceNames
}

// Dependencies supplies runner facts and action/workflow definitions for expression inference.
type Dependencies struct {
	Runners *runners.Knowledge
	// Action returns declared inputs, outputs and runtime for either reference origin.
	// An absent definition means these declarations are unknown.
	Action   func(string) (actionmeta.Definition, error)
	Workflow func(string) (*calls.Metadata, error)
	Log      func(string, ...any)
}

// NewVisitor creates an expression visitor with independent per-workflow context state.
func NewVisitor(dependencies Dependencies, options Options) *Visitor {
	return &Visitor{dependencies: dependencies, options: options}
}

// Configure replaces semantic options before checking a document.
func (rule *Visitor) Configure(options Options) { rule.options = options }

// report is the stored form of an expression finding. Fix alternatives are owned
// by the checker and copied when published to consumers.
type report struct {
	rule, code, message string
	severity            severity.Severity
	start, end          sourcetext.Position
	// hasEnd records whether the expression mapped an inclusive end position.
	hasEnd bool
	fixes  []results.Fix
}

func (r report) finding() *finding.Finding {
	finding := finding.At(&r.start, r.rule, r.message)
	finding.Code = r.code
	finding.Severity = r.severity
	for _, fix := range r.fixes {
		finding.Fixes = append(finding.Fixes, fix.Clone())
	}
	if r.hasEnd {
		end := r.end
		finding.EndAt(&end)
	}
	return finding
}

// Findings returns diagnostics accumulated during workflow expression checking.
// The result is detached from the checker.
func (rule *Visitor) Findings() []*finding.Finding { return rule.FindingsSince(0) }

// FindingCount returns the number of diagnostics accumulated so far. Pass it to
// FindingsSince to read only the diagnostics reported after this point.
func (rule *Visitor) FindingCount() int { return len(rule.errs) }

// FindingsSince returns diagnostics reported after mark, a prior FindingCount.
// The result is detached from the checker.
func (rule *Visitor) FindingsSince(mark int) []*finding.Finding {
	if mark >= len(rule.errs) {
		return nil
	}
	findings := make([]*finding.Finding, 0, len(rule.errs)-mark)
	for _, r := range rule.errs[mark:] {
		findings = append(findings, r.finding())
	}
	return findings
}

func (rule *Visitor) Error(pos *sourcetext.Position, message string) {
	rule.errs = append(rule.errs, report{rule: "expression", message: message, start: *pos})
}

func (rule *Visitor) Errorf(pos *sourcetext.Position, format string, args ...any) {
	rule.Error(pos, fmt.Sprintf(format, args...))
}

func (rule *Visitor) debug(format string, args ...any) {
	if rule.dependencies.Log != nil {
		rule.dependencies.Log(format, args...)
	}
}

func (rule *Visitor) actionDefinition(spec string) (actionmeta.Definition, error) {
	if rule.dependencies.Action == nil {
		return actionmeta.Definition{}, nil
	}
	return rule.dependencies.Action(spec)
}

func (rule *Visitor) workflowMetadata(spec string) (*calls.Metadata, error) {
	if rule.dependencies.Workflow == nil {
		return nil, nil
	}
	return rule.dependencies.Workflow(spec)
}
