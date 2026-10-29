package checks

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/workflow/declaration"

	"actionlint.kjanat.dev/v2/internal/workflow/schema"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

type credentialsCheck struct{ findingCollector }

// Credentials checks container passwords in a complete job declaration.
func Credentials(n declaration.JobView) []Finding {
	rule := &credentialsCheck{name: "credentials"}

	if n.Container().Present() {
		rule.checkContainer("\"container\" section", n.Container())
	}
	if n.Services().Present() {
		if n.Services().Expression().Present() {
			if value, known := expressionLiteral(n.Services().Expression()); known {
				if services, ok := value.(map[string]any); ok {
					for name, container := range services {
						rule.checkContainerValue(fmt.Sprintf("%q service", name), container, viewPosition(n.Services().Expression().Pos()))
					}
				}
			}
		}
		for _, s := range n.Services().Value() {
			rule.checkContainer(fmt.Sprintf("%q service", s.Name().Value()), s.Container())
		}
	}
	return rule.out
}

func (rule *credentialsCheck) checkContainer(where string, n declaration.ContainerView) {
	if n.Expression().Present() {
		if value, known := expressionLiteral(n.Expression()); known {
			rule.checkContainerValue(where, value, viewPosition(n.Expression().Pos()))
		}
		return
	}
	if !n.Credentials().Present() {
		return
	}
	if n.Credentials().Expression().Present() {
		if value, known := expressionLiteral(n.Credentials().Expression()); known {
			rule.checkCredentialsValue(where, value, viewPosition(n.Credentials().Expression().Pos()))
		}
		return
	}

	p := n.Credentials().Password()
	if !p.Present() {
		return
	}
	if value, known := expressionLiteral(p); known {
		rule.checkPasswordValue(where, value, viewPosition(p.Pos()))
	} else if !p.IsExpressionAssigned() {
		rule.reportPassword(where, viewPosition(p.Pos()))
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
