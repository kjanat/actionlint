package engine

import (
	"fmt"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// RuleCredentials is a rule to check credentials in workflows
type RuleCredentials struct {
	RuleBase
}

// NewRuleCredentials creates new RuleCredentials instance
func NewRuleCredentials() *RuleCredentials {
	return &RuleCredentials{
		RuleBase: builtinRuleBase("credentials"),
	}
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleCredentials) VisitJobPre(n *workflowlang.Job) error {
	if n.Container != nil {
		rule.checkContainer("\"container\" section", n.Container)
	}
	if n.Services != nil {
		if n.Services.Expression != nil {
			if value, known := workflowlang.WorkflowExpressionLiteral(n.Services.Expression); known {
				if services, ok := value.(map[string]any); ok {
					for name, container := range services {
						rule.checkContainerValue(fmt.Sprintf("%q service", name), container, n.Services.Expression.Pos)
					}
				}
			}
		}
		for _, s := range n.Services.Value {
			rule.checkContainer(fmt.Sprintf("%q service", s.Name.Value), s.Container)
		}
	}
	return nil
}

func (rule *RuleCredentials) checkContainer(where string, n *workflowlang.Container) {
	if n.Expression != nil {
		if value, known := workflowlang.WorkflowExpressionLiteral(n.Expression); known {
			rule.checkContainerValue(where, value, n.Expression.Pos)
		}
		return
	}
	if n.Credentials == nil {
		return
	}
	if n.Credentials.Expression != nil {
		if value, known := workflowlang.WorkflowExpressionLiteral(n.Credentials.Expression); known {
			rule.checkCredentialsValue(where, value, n.Credentials.Expression.Pos)
		}
		return
	}

	p := n.Credentials.Password
	if p == nil {
		return
	}
	if value, known := workflowlang.WorkflowExpressionLiteral(p); known {
		rule.checkPasswordValue(where, value, p.Pos)
	} else if !p.IsExpressionAssigned() {
		rule.reportPassword(where, p.Pos)
	}
}

func (rule *RuleCredentials) checkContainerValue(where string, value any, pos *workflowlang.Pos) {
	if container, ok := value.(map[string]any); ok {
		rule.checkCredentialsValue(where, workflowObjectProperty(container, "credentials"), pos)
	}
}

func (rule *RuleCredentials) checkCredentialsValue(where string, value any, pos *workflowlang.Pos) {
	if credentials, ok := value.(map[string]any); ok {
		rule.checkPasswordValue(where, workflowObjectProperty(credentials, "password"), pos)
	}
}

func (rule *RuleCredentials) checkPasswordValue(where string, value any, pos *workflowlang.Pos) {
	// Known strings are data, including expression delimiters inside JSON.
	// Invalid value types are reported by expression validation.
	switch value := value.(type) {
	case string:
		if value == "" {
			return
		}
	case float64, bool:
	default:
		return
	}
	rule.reportPassword(where, pos)
}

func (rule *RuleCredentials) reportPassword(where string, pos *workflowlang.Pos) {
	rule.Errorf(pos, "\"password\" section in %s should be specified via secrets. do not put password value directly", where)
}
