package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

// UnsoundTernaryRule is the single definition of the unsound-ternary rule.
var UnsoundTernaryRule = &define.Rule{
	Name:        "unsound-ternary",
	Description: "Report a && b || c expressions where b is always falsy",
	Category:    "correctness",
	Subcategory: "expressions",
	Recommended: true,
	Help:        "The middle operand of a && b || c must be truthy to select b; use an explicit conditional or a truthy representation.",
	DeclaredBy:  "expression",
}
