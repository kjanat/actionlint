package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
)

// ExecutableBitRule is the single definition of the executable-bit rule.
var ExecutableBitRule = &define.Rule{
	Name:        "executable-bit",
	Description: "Report directly invoked repository scripts not marked executable in Git",
	Category:    "correctness",
	Subcategory: "scripts",
	Recommended: true,
	Help:        "Record the executable bit for the referenced script in Git, or invoke it explicitly through its interpreter.",
	Prepare:     prepareExecutableIndex,
	Walker: func(c define.Inputs, base workflowvisit.Base) workflowvisit.Rule {
		return NewRuleExecutableBit(base, ExecutableContext{
			ProjectRoot: c.ProjectRoot, WorkingDir: c.WorkingDir,
			Index: c.ExecutableIndex, Runners: c.Runners,
		})
	},
	Enabled: func(c define.Inputs) bool { return c.ProjectRoot != "" && c.Index != nil },
}
