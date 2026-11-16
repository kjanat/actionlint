package define

import (
	"context"
	"errors"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/results"
)

const (
	defaultProvider     = "actionlint"
	documentationPrefix = "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#"
)

// ScriptCheck analyzes the scripts of one document. It reports findings with
// an empty Rule; the rule's name is attached to them.
type ScriptCheck func(context.Context, inputs.AnalyzerInput) ([]results.Diagnostic, error)

// Rule is the single definition of a native rule.
//
// Name, Description, Category and Subcategory are required. Provider defaults
// to "actionlint", Maturity to the stable maturity, DocumentationURL to the
// rule's section in docs/rules.md, and Explanation to Description written as
// a sentence.
//
// At most one of Checks, Walker and Script may be set. A rule that sets none
// is declaration-only and names in DeclaredBy the component that reports its
// findings.
type Rule struct {
	// Declaration holds the rule's metadata and configuration requirements.
	// Construct its Options with [Options].
	rule.Declaration

	// Prepare acquires facts before construction. Nil means none are needed.
	Prepare func(RuleInputs) (InputPreparation, error)
	// Enabled reports whether the rule runs for these inputs. Nil means always.
	Enabled func(RuleInputs) bool

	// Checks supplies stateless checks of published workflow views. The
	// adapter traverses workflows and, for rules that support actions,
	// composite bodies, and attaches the rule's name to findings without one.
	Checks func(RuleInputs) Checks
	// Walker supplies a stateful traversal. The base carries the rule's name
	// and description and is embedded in the returned visitor.
	Walker func(RuleInputs, workflowvisit.Base) workflowvisit.Rule
	// Script analyzes document scripts.
	Script ScriptCheck

	// DeclaredBy names the component that reports the findings of a
	// declaration-only rule.
	DeclaredBy string
}

// Options returns a constructor of zero option values of type T.
func Options[T any]() func() any {
	return func() any { return new(T) }
}

// Fixed supplies checks that need no inputs.
func Fixed(checks Checks) func(RuleInputs) Checks {
	return func(RuleInputs) Checks { return checks }
}

// Executable reports whether the rule runs anything.
func (r *Rule) Executable() bool {
	return r.Checks != nil || r.Walker != nil || r.Script != nil
}

// Validate reports a definition that cannot be declared or executed.
func (r *Rule) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("rule has no name")
	}
	if r.Description == "" || r.Category == "" || r.Subcategory == "" {
		return errors.New("rule " + r.Name + " needs a description, category and subcategory")
	}
	forms := 0
	for _, present := range []bool{r.Checks != nil, r.Walker != nil, r.Script != nil} {
		if present {
			forms++
		}
	}
	switch {
	case forms > 1:
		return errors.New("rule " + r.Name + " sets more than one of Checks, Walker and Script")
	case forms == 0 && r.DeclaredBy == "":
		return errors.New("rule " + r.Name + " is declaration-only and must name DeclaredBy")
	case forms == 1 && r.DeclaredBy != "":
		return errors.New("rule " + r.Name + " executes itself and must not name DeclaredBy")
	case forms == 0 && (r.Prepare != nil || r.Enabled != nil):
		return errors.New("rule " + r.Name + " is declaration-only and cannot prepare or enable anything")
	}
	return nil
}

// Base returns a fresh traversal accumulator carrying the rule's name and
// description, as [Rule.Walker] receives it.
func (r *Rule) Base() workflowvisit.Base {
	return workflowvisit.NewBase(r.Name, r.Description)
}

// ResolvedDeclaration returns the rule's declaration with defaults applied.
// Each call returns an independent value.
func (r *Rule) ResolvedDeclaration() rule.Declaration {
	declaration := r.Clone()
	if declaration.Maturity == "" {
		declaration.Maturity = rule.StableMaturity
	}
	if declaration.Provider == "" {
		declaration.Provider = defaultProvider
	}
	if declaration.Explanation == "" {
		declaration.Explanation = sentence(declaration.Description)
	}
	if declaration.DocumentationURL == "" {
		declaration.DocumentationURL = documentationPrefix + declaration.Name
	}
	return declaration
}

func sentence(text string) string {
	if text == "" || strings.HasSuffix(text, ".") {
		return text
	}
	return text + "."
}

// NewVisitor constructs the traversal for a rule that executes through Checks
// or Walker. It does not run Prepare or consult Enabled.
func (r *Rule) NewVisitor(in RuleInputs) workflowvisit.Rule {
	return r.Registration(rule.Declaration{}).Build(in)
}

// Registration pairs a rule declaration with its constructor and execution conditions.
type Registration struct {
	rule.Declaration
	// Prepare distinguishes usable inputs, unavailable knowledge and operation
	// failures before construction. Nil means no preparation is needed.
	Prepare func(RuleInputs) (InputPreparation, error)
	Build   func(RuleInputs) workflowvisit.Rule
	Script  inputs.Checker
	Enabled func(RuleInputs) bool
}

// Registration binds the rule's execution to its configuration metadata.
// A declaration-only rule yields a registration with no Build or Script.
func (r *Rule) Registration(metadata rule.Declaration) Registration {
	registration := Registration{Declaration: metadata, Prepare: r.Prepare, Enabled: r.Enabled}
	switch {
	case r.Checks != nil:
		registration.Build = func(c RuleInputs) workflowvisit.Rule {
			return NewChecksVisitor(c.Rules, r.Name, r.Checks(c))
		}
	case r.Walker != nil:
		registration.Build = func(c RuleInputs) workflowvisit.Rule {
			return r.Walker(c, r.Base())
		}
	case r.Script != nil:
		registration.Script = scriptChecker{declaration: r.ResolvedDeclaration(), check: r.Script}
	}
	return registration
}

// scriptChecker exposes a rule's script analysis through the analyzer contract.
type scriptChecker struct {
	declaration rule.Declaration
	check       ScriptCheck
}

func (c scriptChecker) Descriptor() inputs.Descriptor {
	return inputs.Descriptor{Declaration: c.declaration.Clone()}
}

func (c scriptChecker) CheckDocument(ctx context.Context, input inputs.AnalyzerInput) ([]results.Diagnostic, error) {
	diagnostics, err := c.check(ctx, input)
	for i := range diagnostics {
		if diagnostics[i].Rule == "" {
			diagnostics[i].Rule = c.declaration.Name
		}
	}
	return diagnostics, err
}
