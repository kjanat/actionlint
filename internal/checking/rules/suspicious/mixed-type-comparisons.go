package suspicious

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

// MixedTypeComparisonsRule is the single definition of the mixed-type-comparisons rule.
var MixedTypeComparisonsRule = &define.Rule{Name: "mixed-type-comparisons", Description: "Flag condition comparisons that implicitly convert scalar types", Category: "suspicious", Subcategory: "expressions", Recommended: false, Help: "Compare values of matching types or convert them explicitly before comparison.",
	DeclaredBy: "expression",
}
