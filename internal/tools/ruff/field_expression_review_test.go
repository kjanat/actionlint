package ruff

import (
	"strings"
	"testing"
)

func TestFormattedFieldSyntaxTemplates(t *testing.T) {
	end := func(source string) (int, bool) { index := strings.Index(source, "}}"); return index + 2, index >= 0 }
	for _, source := range []string{
		`print(f"{lhs ${{ inputs.operator }} rhs}")`,
		`print(f"{item_${{ inputs.suffix }}}")`,
		`print(f"{${{ inputs.major }}.0}")`,
		`print(f"{[item for ${{ inputs.name }} in values]}")`,
		`print(f"{(lambda ${{ inputs.name }}: 1)()}")`,
		`print(f"{1:{lhs ${{ inputs.operator }} rhs}}")`,
		`print(f"{f'{lhs ${{ inputs.operator }} rhs}'}")`,
		"print(f\"\"\"{lhs\n ${{ inputs.operator }} rhs}\"\"\")",
		"print(f\"\"\"{lhs # comment\n${{ inputs.operator }} rhs}\"\"\")",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			source := strings.ReplaceAll(source, "\n", ending)
			if got, valid, err := Sanitize(source, end); err != nil || valid || got != "" {
				t.Fatalf("syntax template accepted: %q => %q, %v, %v", source, got, valid, err)
			}
		}
	}
	for _, source := range []string{
		`print(f"{${{ inputs.value }}}")`,
		`print(f"{${{ inputs.value }}=}")`,
		`print(f"{${{ inputs.value }} = !r}")`,
		`print(f"{${{ inputs.value }}=:>5}")`,
		`print(f"{lhs == ${{ inputs.value }}}")`,
		`print(f"{${{ inputs.first }} + ${{ inputs.second }}}")`,
		`print(f"{[${{ inputs.value }} for item in values]}")`,
		`print(f"{(lambda item: ${{ inputs.value }})(1)}")`,
		`print(f"{mapping[${{ inputs.value }}]}")`,
		`print(f"{'lhs ${{ inputs.operator }} rhs'}")`,
		`print(f"{lhs:literal ${{ inputs.value }}}")`,
		`print(f"{lhs:{${{ inputs.width }}}}")`,
		`print(f"{f'{lhs == ${{ inputs.value }}}'}")`,
		"print(f\"\"\"{lhs # ${{ inputs.comment }}\n}\"\"\")",
	} {
		if _, valid, err := Sanitize(source, end); err != nil || !valid {
			t.Fatalf("value template skipped: %q, %v, %v", source, valid, err)
		}
	}
}
