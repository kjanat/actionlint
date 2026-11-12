package suspicious

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// MixedTypeMatrixFiltersDeclaration describes the mixed-type-matrix-filters rule.
func MixedTypeMatrixFiltersDeclaration() rule.Declaration {
	return rule.Declaration{Name: "mixed-type-matrix-filters", Description: "Flag matrix filters that compare different or unknown scalar types", Category: "suspicious", Subcategory: "matrix", Maturity: "stable", Provider: "actionlint", Recommended: false, Explanation: "Flag matrix filters that compare different or unknown scalar types.", Help: "Use the same value types in matrix axes and include/exclude filters.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#mixed-type-matrix-filters"}
}
