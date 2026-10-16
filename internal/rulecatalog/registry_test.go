package rulecatalog

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/rule"
)

func TestRuleNamespaceAndDescriptorIsolation(t *testing.T) {
	example := rule.Descriptor{Name: "example", Examples: []rule.Example{{Before: "original"}}, OptionsSchema: []byte(`{}`)}
	rules, err := Combine(example)
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
		if _, err := Combine(input...); err == nil {
			t.Fatalf("ambiguous catalogue accepted: %+v", input)
		}
	}
	reference := string(Reference())
	for _, descriptor := range Rules() {
		if descriptor.Category == "" || descriptor.Subcategory == "" || descriptor.Maturity == "" || descriptor.Provider == "" || descriptor.Help == "" {
			t.Fatalf("incomplete built-in descriptor: %+v", descriptor)
		}
		if !strings.Contains(reference, "lint.rules."+ConfigGroup(descriptor.Name)+"."+descriptor.Name) {
			t.Fatalf("missing configuration path for %s", descriptor.Name)
		}
	}
}
