package security

import (
	"fmt"

	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/model/workflows/knownvalues"
	"actionlint.kjanat.dev/v2/internal/model/workflows/schema"
)

// CredentialsRule is the single definition of the credentials rule.
var CredentialsRule = &define.Rule{
	Name:        "credentials",
	Description: "Detect hardcoded passwords in job and service containers",
	Category:    "security",
	Subcategory: "credentials",
	Recommended: true,
	Help:        "Store the credential as a secret and reference it through the secrets context.",
	Examples: []rule.Example{rule.WorkflowExample("Read container credentials from a secret.",
		"    container:\n      image: ubuntu:latest\n      credentials:\n        username: user\n        password: plaintext\n",
		"    container:\n      image: ubuntu:latest\n      credentials:\n        username: user\n        password: ${{ secrets.REGISTRY_PASSWORD }}\n")},
	Checks: define.Fixed(define.Checks{Job: Credentials}),
}

type credentialsCheck struct{ reporting.Collector }

// Credentials checks container passwords in a complete job declaration.
func Credentials(n workflows.Job) []reporting.StepFinding {
	rule := &credentialsCheck{}

	if !n.Present() {
		return rule.Findings
	}
	rule.checkContainer("\"container\" section", n.Container)
	if n.Services != nil {
		if expression := n.Services.Expression; expression != nil {
			if value, known := knownvalues.Constant(expression); known {
				if services, ok := value.(map[string]any); ok {
					for name, container := range services {
						rule.checkContainerValue(fmt.Sprintf("%q service", name), container, expression.Pos)
					}
				}
			}
		}
		for _, s := range n.Services.Value {
			if s != nil {
				rule.checkContainer(fmt.Sprintf("%q service", s.Name.Text()), s.Container)
			}
		}
	}
	return rule.Findings
}

func (rule *credentialsCheck) checkContainer(where string, n *definition.Container) {
	if n == nil {
		return
	}
	if n.Expression != nil {
		if value, known := knownvalues.Constant(n.Expression); known {
			rule.checkContainerValue(where, value, n.Expression.Pos)
		}
		return
	}
	credentials := n.Credentials
	if credentials == nil {
		return
	}
	if credentials.Expression != nil {
		if value, known := knownvalues.Constant(credentials.Expression); known {
			rule.checkCredentialsValue(where, value, credentials.Expression.Pos)
		}
		return
	}
	p := credentials.Password
	if p == nil {
		return
	}
	if value, known := knownvalues.Constant(p); known {
		rule.checkPasswordValue(where, value, p.Pos)
	} else if !p.IsExpressionAssigned() {
		rule.reportPassword(where, p.Pos)
	}
}

func (rule *credentialsCheck) checkContainerValue(where string, value any, pos *sourcetext.Position) {
	if container, ok := value.(map[string]any); ok {
		rule.checkCredentialsValue(where, schema.Property(container, "credentials"), pos)
	}
}

func (rule *credentialsCheck) checkCredentialsValue(where string, value any, pos *sourcetext.Position) {
	if credentials, ok := value.(map[string]any); ok {
		rule.checkPasswordValue(where, schema.Property(credentials, "password"), pos)
	}
}

func (rule *credentialsCheck) checkPasswordValue(where string, value any, pos *sourcetext.Position) {
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

func (rule *credentialsCheck) reportPassword(where string, pos *sourcetext.Position) {
	rule.Errorf(pos, "\"password\" section in %s should be specified via secrets. do not put password value directly", where)
}
