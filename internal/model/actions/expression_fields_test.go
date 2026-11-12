package action

import (
	"strings"
	"testing"
)

func TestReadExpressionsSeparatesSyntaxFromProblems(t *testing.T) {
	t.Parallel()
	const text = "before ${{ startsWith(true, 'TR') }} between ${{ github.workflow }} after"
	read := ReadExpressions(text, false, "outputs.*.value", nil)
	if len(read.Problems) != 0 || len(read.Expressions) != 2 {
		t.Fatalf("read=%+v", read)
	}
	for index, name := range []string{"startsWith", "github"} {
		expression := read.Expressions[index]
		got := expression.Offset + expression.Node.Token().Offset - 1
		if got != strings.Index(text, name) {
			t.Fatalf("expression %d starts at %d, want %d", index, got, strings.Index(text, name))
		}
	}
	broken := ReadExpressions("${{ github.workflow }} ${{ startsWith( }}", false, "outputs.*.value", nil)
	if len(broken.Expressions) != 1 || len(broken.Problems) != 1 || broken.Problems[0].Code != CodeExpressionSyntax {
		t.Fatalf("earlier syntax was lost after parse failure: %+v", broken)
	}
	folded := ReadExpressions("${{ 'inputs.flag' }}", true, "runs.steps.*.if", nil)
	if len(folded.Expressions) != 1 || folded.Expressions[0].Offset != 0 {
		t.Fatalf("folded syntax acquired an original offset: %+v", folded)
	}
}

func TestMetadataValidationSuppliesExpressionFieldsWithoutPolicy(t *testing.T) {
	t.Parallel()
	const source = "name: example\ndescription: example\noutputs:\n  value:\n    description: value\n    value: ${{ format('{0}', startsWith(true, 'TR')) }}\nruns: {using: composite, steps: []}\n"
	meta, err := Parse("action.yml", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	result := Validate(meta)
	if len(result.Problems) != 0 || len(result.ExpressionFields) != 1 {
		t.Fatalf("validation=%+v", result)
	}
	field := result.ExpressionFields[0]
	if field.SchemaPath != "outputs.*.value" || field.FieldPath != "outputs.value.value" || field.IsKey || len(field.Expressions) != 1 {
		t.Fatalf("wrong expression field: %+v", field)
	}
	if field.Value.Line != 6 || field.Value.Column != 12 || field.Expressions[0].Offset == 0 {
		t.Fatalf("source position lost: %+v", field)
	}
	result.ExpressionFields[0].Value.Value = "changed"
	if next := Validate(meta); len(next.ExpressionFields) != 1 || next.ExpressionFields[0].Value.Value == "changed" {
		t.Fatalf("result changed metadata: %+v", next)
	}
}
