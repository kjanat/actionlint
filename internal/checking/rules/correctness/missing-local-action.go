package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// MissingLocalActionDeclaration describes the missing-local-action rule.
func MissingLocalActionDeclaration() rule.Declaration {
	return rule.Declaration{Name: "missing-local-action", Description: "Report local action references that cannot be resolved", Category: "correctness", Subcategory: "actions", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Report local action references with no manifest or Dockerfile, a missing directory, or an ambiguous path under case-insensitive lookup.", Help: "Check the uses path and ensure the action has a manifest or Dockerfile. If paths differ only in casing, rename them so each path can be resolved unambiguously.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#missing-local-action"}
}
