package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// RunnerSupportDeclaration describes the runner-support rule.
func RunnerSupportDeclaration() rule.Declaration {
	return rule.Declaration{
		Name: "runner-support", Description: "Report runner selections with deprecated or unsupported execution",
		Category: "correctness", Subcategory: "runners", Maturity: "stable", Provider: "actionlint",
		Recommended: true, DefaultLevel: "warn",
		Explanation:      "Report dated support changes established by the selected runner provider for the workflow's execution service. GitHub.com support dates do not apply to GitHub Enterprise Server. Hosted labels such as ubuntu-24.04-arm select ARM64 runners and remain valid. Unknown, stale or incomplete evidence does not establish a support problem.",
		Help:             "Select a supported runner architecture or update the runner inventory. Set repository.service to the service that executes the workflow, and supply the GHES version when known.",
		DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#runner-support",
	}
}
