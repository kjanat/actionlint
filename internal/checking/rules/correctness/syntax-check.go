package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

// SyntaxCheckRule is the single definition of the syntax-check rule.
var SyntaxCheckRule = &define.Rule{ExecutionFailure: true, Name: "syntax-check", Description: "Validate GitHub Actions workflow syntax", Category: "correctness", Subcategory: "syntax", Recommended: true, Help: "Check the indicated YAML field and its expected GitHub Actions value type.", SupportsActions: true,
	DeclaredBy: "workflow parser",
}
