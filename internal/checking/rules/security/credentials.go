package security

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// CredentialsRule is the single definition of the credentials rule.
var CredentialsRule = &define.Rule{Name: "credentials", Description: "Detect hardcoded passwords in job and service containers", Category: "security", Subcategory: "credentials", Recommended: true, Help: "Store the credential as a secret and reference it through the secrets context.", Examples: []rule.Example{rule.WorkflowExample("Read container credentials from a secret.",
	"    container:\n      image: ubuntu:latest\n      credentials:\n        username: user\n        password: plaintext\n",
	"    container:\n      image: ubuntu:latest\n      credentials:\n        username: user\n        password: ${{ secrets.REGISTRY_PASSWORD }}\n")},
	Checks: define.Fixed(define.Checks{Job: Credentials}),
}
