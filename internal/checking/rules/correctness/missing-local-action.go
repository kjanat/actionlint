package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// MissingLocalActionDeclaration describes the missing-local-action rule.
func MissingLocalActionDeclaration() rule.Declaration {
	return rule.Declaration{Name: "missing-local-action", Description: "Report local action references whose manifests cannot be found", Category: "correctness", Subcategory: "actions", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Report local action references whose manifests cannot be found.", Help: "Check the uses path and ensure the local action metadata or Dockerfile exists in the repository.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#missing-local-action"}
}
