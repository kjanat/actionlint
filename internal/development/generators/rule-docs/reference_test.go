package main

import (
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
)

func TestReferenceIncludesEachRuleConfigurationPath(t *testing.T) {
	t.Parallel()
	reference := string(Reference())
	for _, descriptor := range checking.ListRules() {
		if !strings.Contains(reference, "lint.rules."+checking.RuleCategory(descriptor.Name)+"."+descriptor.Name) {
			t.Fatalf("missing configuration path for %s", descriptor.Name)
		}
	}
}
