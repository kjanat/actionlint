package engine

import (
	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/internal/checks/envname"

	"actionlint.kjanat.dev/internal/mapping"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// RuleEnvVar is a rule checker to check environment variables setup.
type RuleEnvVar struct {
	RuleBase
}

// NewRuleEnvVar creates new RuleEnvVar instance.
func NewRuleEnvVar() *RuleEnvVar {
	return &RuleEnvVar{
		RuleBase: builtinRuleBase("env-var"),
	}
}

// VisitStep is callback when visiting Step node.
func (rule *RuleEnvVar) VisitStep(n *workflowlang.Step) error {
	rule.checkEnv(n.Env)
	return nil
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleEnvVar) VisitJobPre(n *workflowlang.Job) error {
	rule.checkEnv(n.Env)
	if n.Container != nil {
		rule.checkEnv(n.Container.Env)
	}
	if n.Services != nil {
		for _, s := range n.Services.Value {
			rule.checkEnv(s.Container.Env)
		}
	}
	return nil
}

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (rule *RuleEnvVar) VisitWorkflowPre(n *workflowlang.Workflow) error {
	rule.checkEnv(n.Env)
	return nil
}

func (rule *RuleEnvVar) checkEnv(env *workflowlang.Env) {
	if env == nil {
		return
	}
	var entries []analyzer.Entry
	workflowlang.InputResolver.Walk(workflowlang.EnvMappingView(env), nil, func(entry mapping.Entry[*workflowlang.String]) {
		if entry.Name != nil {
			entries = append(entries, analyzer.Entry{Name: entry.Name.Value, State: analyzer.KnownKey, Start: factPosition(entry.Name.Pos)})
		}
	})
	for _, finding := range envname.Check(entries) {
		rule.errs = append(rule.errs, legacyDiagnosticError(finding))
	}
}
