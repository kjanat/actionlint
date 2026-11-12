package checking

import (
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

func TestRuleNamespaceAndDescriptorIsolation(t *testing.T) {
	t.Parallel()
	example := rule.Descriptor{Name: "example", Examples: []rule.Example{{Before: "original"}}, OptionsSchema: []byte(`{}`)}
	rules, err := CombineRuleDeclarations(example)
	if err != nil {
		t.Fatal(err)
	}
	for _, descriptor := range rules {
		if descriptor.Name != "example" {
			continue
		}
		descriptor.Examples[0].Before = "changed"
		descriptor.OptionsSchema[0] = 'x'
	}
	if example.Examples[0].Before != "original" || string(example.OptionsSchema) != "{}" {
		t.Fatal("catalogue aliases provider metadata")
	}
	for _, input := range [][]rule.Descriptor{{{Name: "expression"}}, {example, example}, {{Name: " "}}} {
		if _, err := CombineRuleDeclarations(input...); err == nil {
			t.Fatalf("ambiguous catalogue accepted: %+v", input)
		}
	}
	for _, descriptor := range ListRules() {
		if descriptor.Category == "" || descriptor.Subcategory == "" || descriptor.Maturity == "" || descriptor.Provider == "" || descriptor.Help == "" {
			t.Fatalf("incomplete built-in descriptor: %+v", descriptor)
		}
	}
}
