package expressions

import (
	"testing"

	exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestActionOutputInferenceUsesOneDefinition(t *testing.T) {
	t.Parallel()
	definition := actionmeta.ParseDefinition([]byte("name: fixture\ndescription: fixture\noutputs: {result: {description: result, value: ready}}\nruns: {using: composite, steps: []}\n"))
	for _, reference := range []string{"./action", "$/action", "example/action@v1"} {
		checker := New(Dependencies{Action: func(got string) (actionmeta.Definition, error) {
			if got != reference {
				t.Fatalf("reference rewritten by expression inference: %q", got)
			}
			return definition, nil
		}}, Options{})
		outputs := checker.getActionOutputsType(ownedStringFixture(&workflowlang.String{Value: reference}))
		if result, exists := outputs.Property("result"); !exists || result.Kind() != exprtypes.KindString {
			t.Fatalf("%s: missing result output: %v", reference, outputs)
		}
		if _, exists := outputs.Property("undeclared"); exists || !outputs.IsStrict() {
			t.Fatalf("%s: declared outputs became open to undeclared names", reference)
		}
	}
}

func TestUnavailableActionOutputsAreUnknown(t *testing.T) {
	t.Parallel()
	checker := New(Dependencies{}, Options{})
	outputs := checker.getActionOutputsType(ownedStringFixture(&workflowlang.String{Value: "./missing"}))
	if mapped, known := outputs.Mapped(); !known || mapped.Kind() != exprtypes.KindString {
		t.Fatalf("unavailable metadata was treated as no outputs: %v", outputs)
	}
}

func TestActionOutputDeclarationCompleteness(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, source string
		strict       bool
	}{
		{"composite omitted", "runs: {using: composite, steps: []}\n", true},
		{"composite empty", "outputs: {}\nruns: {using: composite, steps: []}\n", true},
		{"node declared", "outputs: {value: {description: value}}\nruns: {using: node24, main: index.js}\n", false},
		{"invalid runtime", "outputs: {}\nruns: {using: unknown}\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			definition := actionmeta.ParseDefinition([]byte("name: fixture\ndescription: fixture\n" + tc.source))
			for _, reference := range []string{"./action", "$/action", "example/action@v1"} {
				checker := New(Dependencies{Action: func(string) (actionmeta.Definition, error) { return definition, nil }}, Options{})
				outputs := checker.getActionOutputsType(ownedStringFixture(&workflowlang.String{Value: reference}))
				if outputs.IsStrict() != tc.strict {
					t.Fatalf("%s: strict=%t, want %t", reference, outputs.IsStrict(), tc.strict)
				}
			}
		})
	}
}
