package builtin

import (
	"errors"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/actioncatalog"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"
	workflowexpr "actionlint.kjanat.dev/v2/internal/workflow/expressions"
	"actionlint.kjanat.dev/v2/internal/workflow/runner"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// RuleExpression connects workflow expression checking to host lookups and options.
type RuleExpression struct {
	*workflowexpr.Checker
	ruleLogger
	Configuration
	compositeScripts bool
}

func (r *RuleExpression) ForComposite(Invocation) (workflowvisit.Rule, error) {
	child := &RuleExpression{Checker: workflowexpr.New(workflowexpr.Dependencies{}, workflowExpressionOptions(r.Config())), compositeScripts: true}
	child.SetConfig(r.Config())
	return child, nil
}

func (r *RuleExpression) VisitStep(step *workflowlang.Step) error {
	if r.compositeScripts {
		r.CheckCompositeScript(step)
		return nil
	}
	return r.Checker.VisitStep(step)
}

func NewRuleExpression(actionsCache actionlocal.MetadataLookup, workflowCache *calls.Cache) *RuleExpression {
	return newRuleExpression(actionsCache, workflowCache, nil)
}

func newRuleExpression(actionsCache actionlocal.MetadataLookup, workflowCache *calls.Cache, runners *runner.Knowledge) *RuleExpression {
	r := &RuleExpression{tag: "expression"}
	r.Checker = workflowexpr.New(workflowexpr.Dependencies{
		Runners: runners,
		LocalAction: func(spec string) (*actionmeta.Metadata, error) {
			if actionsCache == nil {
				return nil, nil
			}
			meta, _, err := actionsCache.FindMetadata(spec)
			// Action validation owns unresolved references and explicit mapping failures.
			if _, unresolved := errors.AsType[actionlocal.UnresolvedError](err); unresolved {
				return nil, nil
			}
			if _, mapping := errors.AsType[actionlocal.MappingError](err); mapping {
				return nil, nil
			}
			return meta, err
		},
		PopularAction: func(spec string) *actionmeta.Metadata { meta, _ := actioncatalog.Lookup(spec); return meta },
		Workflow:      workflowCache.FindMetadataForCall,
		Log:           r.Debug,
	}, workflowExpressionOptions(nil))
	return r
}

func (r *RuleExpression) Name() string                  { return "expression" }
func (r *RuleExpression) Description() string           { return builtinRuleBase("expression").DescriptionText }
func (r *RuleExpression) Errs() []*findingmodel.Finding { return r.Findings() }
func (r *RuleExpression) SetConfig(config *repoconfig.Config) {
	r.Configuration.SetConfig(config)
	r.Configure(workflowExpressionOptions(config))
}

func workflowExpressionOptions(config *repoconfig.Config) workflowexpr.Options {
	return workflowexpr.Options{
		Semantics:               *expressionOptionsFromConfig(config),
		UnsoundTernary:          repoconfig.DiagnosticLevel(config, "unsound-ternary") != "off",
		ScriptInjectionDisabled: repoconfig.DiagnosticLevel(config, "script-injection") == "off",
	}
}

func expressionOptionsFromConfig(config *repoconfig.Config) *exprlang.Options {
	options := &exprlang.Options{Checks: exprlang.Checks{
		StringConditions:          repoconfig.DiagnosticLevel(config, "string-conditions") != "off",
		MixedTypeComparisons:      repoconfig.DiagnosticLevel(config, "mixed-type-comparisons") != "off",
		CaseInsensitiveConditions: repoconfig.DiagnosticLevel(config, "case-insensitive-conditions") != "off",
	}}
	if config != nil {
		options.Variables, options.Secrets = config.ConfigVariables, config.ConfigSecrets
	}
	return options
}
