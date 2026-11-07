package security

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// ScriptInjectionDeclaration describes the script-injection rule.
func ScriptInjectionDeclaration() rule.Declaration {
	return rule.Declaration{Name: "script-injection", Description: "Detect untrusted values and secrets interpolated into scripts", Category: "security", Subcategory: "scripts", Maturity: "stable", Provider: "actionlint", Explanation: "Detect untrusted values and secrets interpolated into scripts.", Help: "Pass untrusted values through environment variables or action inputs. For secrets, prefer the tool's native environment variable or stdin over command arguments. GitHub secrets are automatically masked; register generated or fetched secrets with add-mask before use. Masking protects logs, not process arguments.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#script-injection", Recommended: true, Examples: []rule.Example{
		rule.LabeledWorkflowExample("Prefer the tool's native environment variable: GitHub CLI reads GH_TOKEN directly, keeping the secret out of script text and command arguments. GitHub secrets are automatically masked in logs; register generated or fetched secrets with add-mask before use. Masking does not protect process arguments.",
			"wrong: secret in script text", "correct: secret in environment variable",
			"    steps:\n      - run: GH_TOKEN=\"${{ secrets.TOKEN }}\" gh api user\n",
			"    steps:\n      - env:\n          GH_TOKEN: ${{ secrets.TOKEN }}\n        run: gh api user\n"),
		rule.LabeledWorkflowExample("When supported, pass secrets through stdin. Bash's printf builtin feeds Docker's password-stdin option without putting the password in Docker's arguments. GitHub masks this stored secret automatically; generated or fetched values must be masked before use. Keep shell tracing disabled.",
			"wrong: password in command arguments", "correct: password through stdin",
			"    steps:\n      - env:\n          REGISTRY_USER: ${{ vars.REGISTRY_USER }}\n        run: docker login --username \"$REGISTRY_USER\" --password \"${{ secrets.REGISTRY_PASSWORD }}\"\n",
			"    steps:\n      - shell: bash\n        env:\n          REGISTRY_USER: ${{ vars.REGISTRY_USER }}\n          REGISTRY_PASSWORD: ${{ secrets.REGISTRY_PASSWORD }}\n        run: printf '%s' \"${REGISTRY_PASSWORD}\" | docker login --username \"${REGISTRY_USER}\" --password-stdin\n"),
	}, SupportsActions: true}
}
