package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// ScriptInterpolationDeclaration describes the script-interpolation rule.
func ScriptInterpolationDeclaration() rule.Declaration {
	return rule.Declaration{Name: "script-interpolation", Description: "Detect template values that may change script syntax", Category: "correctness", Subcategory: "scripts", Maturity: "stable", Provider: "actionlint", Explanation: "Detect template values that may change script syntax. Unknown strings are checked independently of attacker control. Constrained, nonempty literal strings can be used as shell words; empty and all-decimal strings need a proved data-argument position. Booleans, numeric values, optional matrix members, numeric identifiers, SHA values, and hashFiles results also require that position.", Help: "Pass dynamic values through environment variables and read them as data in the script. Quoting a template expression does not prevent substitution before script parsing.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#script-interpolation", Recommended: true, Examples: []rule.Example{rule.LabeledWorkflowExample("Keep a dynamic value out of generated shell source.",
		"wrong: template value in script text", "better: template value in environment variable",
		"    steps:\n      - run: echo \"${{ vars.MESSAGE }}\"\n",
		"    steps:\n      - env: { MESSAGE: \"${{ vars.MESSAGE }}\" }\n        run: echo \"${MESSAGE}\"\n")}, SupportsActions: true}
}
