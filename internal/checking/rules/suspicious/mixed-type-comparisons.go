package suspicious

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

var MixedTypeComparisonsRule = &define.Rule{
	Name:        "mixed-type-comparisons",
	Description: "Flag condition comparisons that implicitly convert scalar types",
	Category:    "suspicious",
	Subcategory: "expressions",
	Help:        "Compare values of the same type, or convert one explicitly.",
	DeclaredBy:  "expression",
}
