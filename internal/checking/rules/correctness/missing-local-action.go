package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

// MissingLocalActionRule is the single definition of the missing-local-action rule.
var MissingLocalActionRule = &define.Rule{Name: "missing-local-action", Description: "Report local action references that cannot be resolved", Category: "correctness", Subcategory: "actions", Recommended: true, Explanation: "Report local action references with no manifest or Dockerfile, a missing directory, or an ambiguous path under case-insensitive lookup.", Help: "Check the uses path and ensure the action has a manifest or Dockerfile. If paths differ only in casing, rename them so each path can be resolved unambiguously.",
	DeclaredBy: "action",
}
