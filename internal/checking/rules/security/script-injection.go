package security

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

var ScriptInjectionRule = &define.Rule{
	Name:        "script-injection",
	Description: "Detect untrusted values and secrets interpolated into scripts",
	Category:    "security",
	Subcategory: "scripts",
	Help:        "Pass untrusted values through environment variables or action inputs. Pass secrets through the tool's own environment variable or stdin. Register generated or fetched secrets with add-mask before use.",
	Recommended: true,
	Examples: []rule.Example{
		rule.LabeledWorkflowExample("GitHub CLI reads GH_TOKEN from the environment.",
			"wrong: secret in script text", "correct: secret in environment variable",
			"    steps:\n      - run: GH_TOKEN=\"${{ secrets.TOKEN }}\" gh api user\n",
			"    steps:\n      - env:\n          GH_TOKEN: ${{ secrets.TOKEN }}\n        run: gh api user\n"),
		rule.LabeledWorkflowExample("Pass the password to docker login through stdin. Keep shell tracing disabled.",
			"wrong: password in command arguments", "correct: password through stdin",
			"    steps:\n      - env:\n          REGISTRY_USER: ${{ vars.REGISTRY_USER }}\n        run: docker login --username \"$REGISTRY_USER\" --password \"${{ secrets.REGISTRY_PASSWORD }}\"\n",
			"    steps:\n      - shell: bash\n        env:\n          REGISTRY_USER: ${{ vars.REGISTRY_USER }}\n          REGISTRY_PASSWORD: ${{ secrets.REGISTRY_PASSWORD }}\n        run: printf '%s' \"${REGISTRY_PASSWORD}\" | docker login --username \"${REGISTRY_USER}\" --password-stdin\n"),
	},
	SupportsActions: true,
	DeclaredBy:      "expression",
}

// ScriptInjection skips the invalid secret selectors in semanticErrors and
// keeps findings in other branches.
func ScriptInjection(expr syntax.Node, semanticErrors []*typecheck.Error) []*typecheck.Error {
	findings := checkUntrustedInputs(expr)
	return append(findings, checkSecretInterpolations(expr, semanticErrors)...)
}
