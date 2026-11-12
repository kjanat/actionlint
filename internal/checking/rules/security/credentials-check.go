package security

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	workflowvalues "actionlint.kjanat.dev/v2/internal/model/workflows/values"

	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/schema"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

type credentialsCheck struct{ reporting.Collector }

// Credentials checks container passwords in a complete job declaration.
func Credentials(n declaration.JobView) []reporting.Finding {
	rule := &credentialsCheck{Rule: "credentials"}

	if n.Container().Present() {
		rule.checkContainer("\"container\" section", n.Container())
	}
	if n.Services().Present() {
		if n.Services().Expression().Present() {
			if value, known := workflowvalues.Constant(n.Services().Expression()); known {
				if services, ok := value.(map[string]any); ok {
					for name, container := range services {
						rule.checkContainerValue(fmt.Sprintf("%q service", name), container, reporting.ViewPosition(n.Services().Expression().Pos()))
					}
				}
			}
		}
		for _, s := range n.Services().Value() {
			rule.checkContainer(fmt.Sprintf("%q service", s.Name().Value()), s.Container())
		}
	}
	return rule.Findings
}

func (rule *credentialsCheck) checkContainer(where string, n declaration.ContainerView) {
	if n.Expression().Present() {
		if value, known := workflowvalues.Constant(n.Expression()); known {
			rule.checkContainerValue(where, value, reporting.ViewPosition(n.Expression().Pos()))
		}
		return
	}
	if !n.Credentials().Present() {
		return
	}
	if n.Credentials().Expression().Present() {
		if value, known := workflowvalues.Constant(n.Credentials().Expression()); known {
			rule.checkCredentialsValue(where, value, reporting.ViewPosition(n.Credentials().Expression().Pos()))
		}
		return
	}

	p := n.Credentials().Password()
	if !p.Present() {
		return
	}
	if value, known := workflowvalues.Constant(p); known {
		rule.checkPasswordValue(where, value, reporting.ViewPosition(p.Pos()))
	} else if !p.IsExpressionAssigned() {
		rule.reportPassword(where, reporting.ViewPosition(p.Pos()))
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
