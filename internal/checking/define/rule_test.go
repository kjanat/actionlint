package define

import (
	"context"
	"strings"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	rule "actionlint.kjanat.dev/v2/internal/model/rules"
	"actionlint.kjanat.dev/v2/internal/results"
)

type exampleOptions struct{ Limit int }

func TestDeclarationAppliesDefaults(t *testing.T) {
	t.Parallel()
	native := &Rule{Name: "example", Description: "Report examples", Category: "correctness", Subcategory: "tests", Options: Options[exampleOptions]()}
	got := native.ResolvedDeclaration()
	if got.Provider != "actionlint" || got.Maturity != rule.StableMaturity {
		t.Fatalf("provider %q maturity %q", got.Provider, got.Maturity)
	}
	if got.Explanation != "Report examples." {
		t.Fatalf("explanation defaults to the description as a sentence, got %q", got.Explanation)
	}
	if want := "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#example"; got.DocumentationURL != want {
		t.Fatalf("documentation URL %q, want %q", got.DocumentationURL, want)
	}
	if _, ok := got.Options().(*exampleOptions); !ok {
		t.Fatalf("options constructor returned %T", got.Options())
	}
}

func TestDeclarationKeepsExplicitValues(t *testing.T) {
	t.Parallel()
	native := &Rule{
		Name: "example", Description: "Report examples", Category: "correctness", Subcategory: "tests",
		Provider: "plugin", Maturity: rule.NurseryMaturity, Explanation: "Longer text.", DocumentationURL: "https://example.com/rule",
	}
	got := native.ResolvedDeclaration()
	if got.Provider != "plugin" || got.Maturity != rule.NurseryMaturity || got.Explanation != "Longer text." || got.DocumentationURL != "https://example.com/rule" {
		t.Fatalf("explicit values were replaced: %+v", got.Descriptor)
	}
}

func TestDeclarationIsDetached(t *testing.T) {
	t.Parallel()
	native := &Rule{Name: "example", Description: "d", Category: "c", Subcategory: "s", Aliases: []rule.Alias{{Name: "old"}}}
	first := native.ResolvedDeclaration()
	first.Aliases[0].Name = "changed"
	if native.ResolvedDeclaration().Aliases[0].Name != "old" {
		t.Fatal("caller changed the rule's aliases")
	}
}

func TestValidateRejectsAmbiguousDefinitions(t *testing.T) {
	t.Parallel()
	base := Rule{Name: "example", Description: "d", Category: "c", Subcategory: "s"}
	checks := func(RuleInputs) Checks { return Checks{} }
	walker := func(RuleInputs, workflowvisit.Base) workflowvisit.Rule { return nil }
	script := func(context.Context, checkinputs.AnalyzerInput) ([]results.Diagnostic, error) { return nil, nil }
	tests := map[string]struct {
		edit    func(*Rule)
		wantErr string
	}{
		"checks only":            {func(r *Rule) { r.Checks = checks }, ""},
		"walker only":            {func(r *Rule) { r.Walker = walker }, ""},
		"script only":            {func(r *Rule) { r.Script = script }, ""},
		"declaration only":       {func(r *Rule) { r.DeclaredBy = "expression" }, ""},
		"no form and no emitter": {func(*Rule) {}, "DeclaredBy"},
		"checks and walker":      {func(r *Rule) { r.Checks, r.Walker = checks, walker }, "more than one"},
		"executable and emitter": {func(r *Rule) { r.Checks, r.DeclaredBy = checks, "expression" }, "must not name DeclaredBy"},
		"declared with enable":   {func(r *Rule) { r.DeclaredBy, r.Enabled = "expression", func(RuleInputs) bool { return true } }, "cannot prepare or enable"},
		"no name":                {func(r *Rule) { r.Name, r.Checks = "", checks }, "no name"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			native := base
			test.edit(&native)
			err := native.Validate()
			switch {
			case test.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)):
				t.Fatalf("error %v does not mention %q", err, test.wantErr)
			}
		})
	}
}

func TestRegistrationAttachesRuleNameToScriptFindings(t *testing.T) {
	t.Parallel()
	native := &Rule{
		Name: "example", Description: "d", Category: "c", Subcategory: "s",
		Script: func(context.Context, checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
			return []results.Diagnostic{{Message: "unnamed"}, {Rule: "other", Message: "named"}}, nil
		},
	}
	registration := native.Registration(rule.Declaration{})
	if registration.Build != nil || registration.Script == nil {
		t.Fatalf("script rule registered as %+v", registration)
	}
	got, err := registration.Script.CheckDocument(t.Context(), checkinputs.AnalyzerInput{})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Rule != "example" || got[1].Rule != "other" {
		t.Fatalf("rule names %q and %q", got[0].Rule, got[1].Rule)
	}
}

func TestDeclarationOnlyRuleRegistersNoExecution(t *testing.T) {
	t.Parallel()
	native := &Rule{Name: "example", Description: "d", Category: "c", Subcategory: "s", DeclaredBy: "expression"}
	registration := native.Registration(rule.Declaration{})
	if native.Executable() || registration.Build != nil || registration.Script != nil {
		t.Fatalf("declaration-only rule executes: %+v", registration)
	}
}
