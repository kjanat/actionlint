package security

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// CredentialsDeclaration describes the credentials rule.
func CredentialsDeclaration() rule.Declaration {
	return rule.Declaration{Name: "credentials", Description: "Detect hardcoded passwords in job and service containers", Category: "security", Subcategory: "credentials", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Detect hardcoded passwords in job and service containers.", Help: "Store the credential as a secret and reference it through the secrets context.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#credentials", Examples: []rule.Example{rule.WorkflowExample("Read container credentials from a secret.",
		"    container:\n      image: ubuntu:latest\n      credentials:\n        username: user\n        password: plaintext\n",
		"    container:\n      image: ubuntu:latest\n      credentials:\n        username: user\n        password: ${{ secrets.REGISTRY_PASSWORD }}\n")}}
}
