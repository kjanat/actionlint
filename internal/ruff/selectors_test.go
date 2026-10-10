package ruff

import (
	"slices"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestSupportedRuleSelectors(t *testing.T) {
	selectors := SupportedRuleSelectors()
	if !slices.IsSorted(selectors) || len(slices.Compact(slices.Clone(selectors))) != len(selectors) {
		t.Fatal("selector metadata must be sorted and unique")
	}
	for _, selector := range selectors {
		var cfg Config
		if err := yaml.Unmarshal([]byte("select: ["+selector+"]\nignore: ["+selector+"]"), &cfg); err != nil {
			t.Fatalf("published selector %s is rejected: %v", selector, err)
		}
	}
	selectors[0] = "XYZ"
	if SupportedRuleSelectors()[0] == "XYZ" {
		t.Fatal("returned selectors must not mutate the parser's inventory")
	}
}
