package checks

import (
	"fmt"

	"actionlint.kjanat.dev/internal/workflow/schema"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

type credentialsCheck struct{ findingCollector }

// Credentials checks container passwords in a complete job declaration.
func Credentials(n *workflowlang.Job) []Finding {
	rule := &credentialsCheck{name: "credentials"}

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
	return rule.out
}

func (rule *credentialsCheck) checkContainer(where string, n *workflowlang.Container) {
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

func (rule *credentialsCheck) checkContainerValue(where string, value any, pos *workflowlang.Pos) {
	if container, ok := value.(map[string]any); ok {
		rule.checkCredentialsValue(where, schema.Property(container, "credentials"), pos)
	}
}

func (rule *credentialsCheck) checkCredentialsValue(where string, value any, pos *workflowlang.Pos) {
	if credentials, ok := value.(map[string]any); ok {
		rule.checkPasswordValue(where, schema.Property(credentials, "password"), pos)
	}
}

func (rule *credentialsCheck) checkPasswordValue(where string, value any, pos *workflowlang.Pos) {
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

func (rule *credentialsCheck) reportPassword(where string, pos *workflowlang.Pos) {
	rule.Errorf(pos, "\"password\" section in %s should be specified via secrets. do not put password value directly", where)
}
