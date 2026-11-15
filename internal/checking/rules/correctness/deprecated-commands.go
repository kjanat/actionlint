package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

// DeprecatedCommandsRule is the single definition of the deprecated-commands rule.
var DeprecatedCommandsRule = &define.Rule{
	Name:            "deprecated-commands",
	Description:     "Report deprecated set-output, save-state, set-env, and add-path commands",
	Category:        "correctness",
	Subcategory:     "scripts",
	Recommended:     true,
	Help:            "Write values to the corresponding quoted GITHUB_OUTPUT, GITHUB_STATE, GITHUB_ENV or GITHUB_PATH file.",
	SupportsActions: true,
	Scope:           rule.SourceConfiguration,
	RequiredInputs:  []rule.InputKind{rule.Scripts, rule.Invocations},
	Script:          checkDeprecatedCommands,
}
