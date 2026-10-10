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

func TestUnavailableSelectorsRejected(t *testing.T) {
	for _, selector := range []string{"E111", "RUF055", "PLR0904", "ANN101", "ANN102", "S320"} {
		for _, key := range []string{"select", "ignore"} {
			var cfg Config
			if err := yaml.Unmarshal([]byte(key+": ["+selector+"]"), &cfg); err == nil {
				t.Fatalf("accepted unavailable %s selector %s", key, selector)
			}
		}
	}
	for _, selector := range []string{"CPY001", "E", "RUF", "F", "C9", "U004", "SIM111", "PGH001", "PGH002", "PLR1701", "RUF011", "RUF035", "TRY200"} {
		var cfg Config
		if err := yaml.Unmarshal([]byte("select: ["+selector+"]"), &cfg); err != nil {
			t.Fatalf("stable selector %s rejected: %v", selector, err)
		}
	}
}
