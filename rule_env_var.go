package actionlint

import (
	"strings"

	"actionlint.kjanat.dev/internal/mapping"
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
func (rule *RuleEnvVar) VisitStep(n *Step) error {
	rule.checkEnv(n.Env)
	return nil
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleEnvVar) VisitJobPre(n *Job) error {
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
func (rule *RuleEnvVar) VisitWorkflowPre(n *Workflow) error {
	rule.checkEnv(n.Env)
	return nil
}

func (rule *RuleEnvVar) checkEnv(env *Env) {
	if env == nil {
		return
	}
	inputResolver.Walk(envMappingView(env), nil, func(entry mapping.Entry[*String]) {
		if entry.Name != nil {
			rule.checkName(entry.Name.Value, entry.Name.Pos)
		}
	})
}

func (rule *RuleEnvVar) checkName(name string, pos *Pos) {
	if strings.ContainsAny(name, "&= \t") {
		rule.Errorf(pos, "environment variable name %q is invalid. '&', '=' and spaces should not be contained", name)
	}
}
