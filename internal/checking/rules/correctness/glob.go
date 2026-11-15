package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

// GlobRule is the single definition of the glob rule.
var GlobRule = &define.Rule{Name: "glob", Description: "Validate branch, tag, and path filter patterns", Category: "correctness", Subcategory: "filters", Recommended: true, Help: "Correct the pattern syntax and check how positive and negative patterns are ordered.",
	Checks: define.Fixed(define.Checks{Workflow: GlobWorkflow, Job: GlobJob}),
}
