package suspicious

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
)

var MixedTypeMatrixFiltersRule = &define.Rule{
	Name:        "mixed-type-matrix-filters",
	Description: "Flag matrix filters that compare different or unknown scalar types",
	Category:    "suspicious",
	Subcategory: "matrix",
	Help:        "Use the same value types in matrix axes and include/exclude filters.",
	DeclaredBy:  "matrix",
}
