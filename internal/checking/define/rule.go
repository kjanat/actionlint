package define

import (
	"context"
	"errors"
	"slices"
	"strings"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/results"
)

const (
	defaultProvider     = "actionlint"
	documentationPrefix = "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#"
)

// ScriptCheck analyzes the scripts of one document. It reports findings with
// an empty Rule; the rule's name is attached to them.
type ScriptCheck func(context.Context, checkinputs.AnalyzerInput) ([]results.Diagnostic, error)

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
	Name             string
	Aliases          []rule.Alias
	Description      string
	Category         string
	Subcategory      string
	Maturity         string
	Provider         string
	Recommended      bool
	Explanation      string
	Help             string
	DocumentationURL string
	Examples         []rule.Example
	RequiredInputs   []rule.InputKind

	ExecutionFailure bool
	DefaultLevel     string
	DefaultEnabled   *bool
	SupportsActions  bool
	Scope            rule.ConfigurationScope

	// Options constructs a fresh options value for decoding and schema
	// generation. Use [Options]. Nil means the rule accepts no options.
	Options func() any

	// LegacyGroup, Policy, MovedOptions and MigrationNote describe how
	// configuration written for earlier versions selects the rule. See
	// [rule.Declaration].
	LegacyGroup   string
	Policy        string
	MovedOptions  []rule.MovedOption
	MigrationNote string

	// Prepare acquires facts before construction. Nil means none are needed.
	Prepare func(Inputs) (InputPreparation, error)
	// Enabled reports whether the rule runs for these inputs. Nil means always.
	Enabled func(Inputs) bool

	// Checks supplies stateless checks of published workflow views. The
	// adapter traverses workflows and, for rules that support actions,
	// composite bodies, and attaches the rule's name to findings without one.
	Checks func(Inputs) Checks
	// Walker supplies a stateful traversal. The base carries the rule's name
	// and description and is embedded in the returned visitor.
	Walker func(Inputs, workflowvisit.Base) workflowvisit.Rule
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
func Fixed(checks Checks) func(Inputs) Checks {
	return func(Inputs) Checks { return checks }
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

// Declaration returns the rule's declaration with defaults applied. Each call
// returns an independent value.
func (r *Rule) Declaration() rule.Declaration {
	descriptor := rule.Descriptor{
		ExecutionFailure: r.ExecutionFailure,
		DefaultLevel:     r.DefaultLevel,
		DefaultEnabled:   r.DefaultEnabled,
		Name:             r.Name,
		Aliases:          r.Aliases,
		Description:      r.Description,
		Category:         r.Category,
		Subcategory:      r.Subcategory,
		Maturity:         r.Maturity,
		Provider:         r.Provider,
		Recommended:      r.Recommended,
		Explanation:      r.Explanation,
		Help:             r.Help,
		DocumentationURL: r.DocumentationURL,
		Examples:         r.Examples,
		RequiredInputs:   r.RequiredInputs,
	}
	if descriptor.Maturity == "" {
		descriptor.Maturity = rule.StableMaturity
	}
	if descriptor.Provider == "" {
		descriptor.Provider = defaultProvider
	}
	if descriptor.Explanation == "" {
		descriptor.Explanation = sentence(r.Description)
	}
	if descriptor.DocumentationURL == "" {
		descriptor.DocumentationURL = documentationPrefix + r.Name
	}
	return rule.Declaration{
		Descriptor: descriptor.Clone(), SupportsActions: r.SupportsActions, Scope: r.Scope, Options: r.Options,
		LegacyGroup: r.LegacyGroup, Policy: r.Policy, MovedOptions: slices.Clone(r.MovedOptions), MigrationNote: r.MigrationNote,
	}
}

func sentence(text string) string {
	if text == "" || strings.HasSuffix(text, ".") {
		return text
	}
	return text + "."
}

// NewVisitor constructs the traversal for a rule that executes through Checks
// or Walker. It does not run Prepare or consult Enabled.
func (r *Rule) NewVisitor(in Inputs) workflowvisit.Rule {
	return r.Registration(configuration.Rule{}).Build(in)
}

// Registration pairs a rule declaration with its constructor and execution conditions.
type Registration struct {
	configuration.Rule
	// Prepare distinguishes usable inputs, unavailable knowledge and operation
	// failures before construction. Nil means no preparation is needed.
	Prepare func(Inputs) (InputPreparation, error)
	Build   func(Inputs) workflowvisit.Rule
	Script  checkinputs.Checker
	Enabled func(Inputs) bool
}

// Registration binds the rule's execution to its configuration metadata.
// A declaration-only rule yields a registration with no Build or Script.
func (r *Rule) Registration(metadata configuration.Rule) Registration {
	registration := Registration{Rule: metadata, Prepare: r.Prepare, Enabled: r.Enabled}
	switch {
	case r.Checks != nil:
		registration.Build = func(c Inputs) workflowvisit.Rule {
			return NewChecksVisitor(c.Rules, r.Name, r.Checks(c))
		}
	case r.Walker != nil:
		registration.Build = func(c Inputs) workflowvisit.Rule {
			return r.Walker(c, r.Base())
		}
	case r.Script != nil:
		registration.Script = scriptChecker{declaration: r.Declaration(), check: r.Script}
	}
	return registration
}

// scriptChecker exposes a rule's script analysis through the analyzer contract.
type scriptChecker struct {
	declaration rule.Declaration
	check       ScriptCheck
}

func (c scriptChecker) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Descriptor: c.declaration.Clone(), Scope: c.declaration.Scope, SupportsActions: c.declaration.SupportsActions}
}

func (c scriptChecker) CheckDocument(ctx context.Context, input checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
	diagnostics, err := c.check(ctx, input)
	for i := range diagnostics {
		if diagnostics[i].Rule == "" {
			diagnostics[i].Rule = c.declaration.Name
		}
	}
	return diagnostics, err
}
