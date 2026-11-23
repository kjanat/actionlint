package define

import (
	"context"
	"errors"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/results"
)

const (
	defaultProvider     = "actionlint"
	documentationPrefix = "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#"
)

// ScriptCheck reports findings with an empty Rule, and the adapter attaches the rule's name.
type ScriptCheck func(context.Context, checkinputs.AnalyzerInput) ([]results.Diagnostic, error)

// Rule requires Name, Description, Category and Subcategory, and sets at most one of Checks, Walker and Script.
type Rule struct {
	// Construct Declaration.Options with [Options].
	rule.Declaration

	Prepare func(RuleInputs) (InputPreparation, error)
	Enabled func(RuleInputs) bool

	Checks func(RuleInputs) Checks
	// Walker must embed the base in the returned visitor.
	Walker func(RuleInputs, workflowvisit.Base) workflowvisit.Rule
	Script ScriptCheck

	// DeclaredBy names the component that reports a declaration-only rule's findings.
	DeclaredBy string
}

func Options[T any]() func() any {
	return func() any { return new(T) }
}

func Fixed(checks Checks) func(RuleInputs) Checks {
	return func(RuleInputs) Checks { return checks }
}

func (r *Rule) Executable() bool {
	return r.Checks != nil || r.Walker != nil || r.Script != nil
}

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

func (r *Rule) Base() workflowvisit.Base {
	return workflowvisit.NewBase(r.Name, r.Description)
}

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

func (r *Rule) Visits() bool { return r.Checks != nil || r.Walker != nil }

// NewVisitor skips Prepare and Enabled, and returns nil for a rule that does not visit.
func (r *Rule) NewVisitor(in RuleInputs) workflowvisit.Rule {
	switch {
	case r.Checks != nil:
		return NewChecksVisitor(in.Rules, r.Name, r.Checks(in))
	case r.Walker != nil:
		return r.Walker(in, r.Base())
	default:
		return nil
	}
}

func (r *Rule) ScriptAnalyzer() (results.Analyzer, bool) {
	if r.Script == nil {
		return nil, false
	}
	return scriptAnalyzer{declaration: r.ResolvedDeclaration(), analyze: r.Script}, true
}

// Rule's Descriptor field would collide with the analyzer contract's Descriptor method.
type scriptAnalyzer struct {
	declaration rule.Declaration
	analyze     ScriptCheck
}

func (a scriptAnalyzer) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Declaration: a.declaration.Clone()}
}

func (a scriptAnalyzer) AnalyzeDocument(ctx context.Context, input checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
	diagnostics, err := a.analyze(ctx, input)
	for i := range diagnostics {
		if diagnostics[i].Rule == "" {
			diagnostics[i].Rule = a.declaration.Name
		}
	}
	return diagnostics, err
}
