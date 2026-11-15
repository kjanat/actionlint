package suspicious

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

// CaseInsensitiveConditionsRule is the single definition of the case-insensitive-conditions rule.
var CaseInsensitiveConditionsRule = &define.Rule{Name: "case-insensitive-conditions", Description: "Flag case-insensitive identity comparisons", Category: "suspicious", Subcategory: "expressions", Recommended: false, Help: "Expression string comparisons ignore case; avoid using them as case-sensitive identity checks.",
	DeclaredBy: "expression",
}
