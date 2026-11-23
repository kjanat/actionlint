package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

var SyntaxCheckRule = &define.Rule{
	ExecutionFailure: true,
	Name:             "syntax-check",
	Description:      "Validate GitHub Actions workflow and action metadata syntax",
	Category:         "correctness",
	Subcategory:      "syntax",
	Recommended:      true,
	Help:             "Give the reported field a value of its expected type.",
	SupportsActions:  true,
	DeclaredBy:       "workflow parser and action metadata traversal",
}
