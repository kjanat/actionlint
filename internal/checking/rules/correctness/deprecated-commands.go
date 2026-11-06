package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// DeprecatedCommandsDeclaration describes the deprecated-commands rule.
func DeprecatedCommandsDeclaration() rule.Declaration {
	return rule.Declaration{Name: "deprecated-commands", Description: "Report deprecated set-output, save-state, set-env, and add-path commands", Category: "correctness", Subcategory: "scripts", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Report deprecated set-output, save-state, set-env, and add-path commands.", Help: "Write values to the corresponding quoted GITHUB_OUTPUT, GITHUB_STATE, GITHUB_ENV or GITHUB_PATH file.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#deprecated-commands", SupportsActions: true, Scope: rule.SourceConfiguration, RequiredInputs: []rule.InputKind{rule.Scripts, rule.Invocations}}
}
