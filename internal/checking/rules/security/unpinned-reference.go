package security

import (
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	lintconfig "actionlint.kjanat.dev/v2/internal/input/configuration/rules"
)

// UnpinnedReferenceDeclaration describes the unpinned-reference rule.
func UnpinnedReferenceDeclaration() rule.Declaration {
	return rule.Declaration{Name: "unpinned-reference", Aliases: []rule.Alias{{Name: "require-commit-hash", Deprecated: true}}, Description: "Require full commit SHAs for actions and workflows, and digests for images", Category: "security", Subcategory: "dependencies", Maturity: "stable", Provider: "actionlint", Recommended: false, Explanation: "Require full commit SHAs for actions and workflows, and digests for images.", Help: "Pin uses references to a full commit SHA and container images to a digest.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#unpinned-reference", Options: func() any { return &lintconfig.CommitHashOptions{} }}
}
