package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

var MissingLocalActionRule = &define.Rule{
	SupportsActions: true,
	Name:            "missing-local-action",
	Description:     "Report local action references that cannot be resolved",
	Category:        "correctness",
	Subcategory:     "actions",
	Recommended:     true,
	Explanation:     "Report local action references with no manifest or Dockerfile, a missing directory, or an ambiguous path under case-insensitive lookup.",
	Help:            "Fix the uses path, add a manifest or Dockerfile, or rename paths that differ only in case.",
	DeclaredBy:      "action metadata traversal",
}
