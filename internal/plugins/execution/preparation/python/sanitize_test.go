package python

import (
	"strings"
	"testing"
)

func TestSanitize(t *testing.T) {
	source := "print(${{\n 'é'\n}}, missing)"
	got, valid, err := Sanitize(source, func(string) (int, bool) { return len("\n 'é'\n}}"), true })
	if err != nil || !valid || len([]rune(got)) != len([]rune(source)) || strings.Contains(got, "${{") {
		t.Fatalf("%q %v", got, valid)
	}
	for i, r := range []rune(source) {
		if r == '\n' && []rune(got)[i] != r {
			t.Fatal("line break moved")
		}
	}
	for _, end := range []int{-1, 0, 1, len(source) + 1} {
		if _, valid, _ := Sanitize(source, func(string) (int, bool) { return end, true }); valid {
			t.Fatalf("accepted end %d", end)
		}
	}
}

func TestSanitizeTemplateContexts(t *testing.T) {
	end := func(source string) (int, bool) {
		index := strings.Index(source, "}}")
		return index + 2, index >= 0
	}
	for _, source := range []string{
		"# ${{\n\n value\n}} trailing\nprint(missing)",
		"print(${{\n\n value\n}})\nprint(missing)",
		"print('''${{\n\n value\n}}''')\nprint(missing)",
		"print(\"# ${{\n value\n}}\")\nprint(missing)",
		"print('${{ value }}') # ${{\n value\n}}\nprint(missing)",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			source := strings.ReplaceAll(source, "\n", ending)
			t.Run(source, func(t *testing.T) {
				got, valid, err := Sanitize(source, end)
				if err != nil || !valid || len([]rune(got)) != len([]rune(source)) {
					t.Fatalf("got %q, valid %v, error %v", got, valid, err)
				}
				for i, r := range []rune(source) {
					if (r == '\n' || r == '\r') && []rune(got)[i] != r {
						t.Fatal("line ending moved")
					}
				}
				if !strings.HasSuffix(got, "print(missing)") {
					t.Fatal("unrelated code changed")
				}
			})
		}
	}
}

func TestSanitizeAdjacentPythonDelimiters(t *testing.T) {
	end := func(source string) (int, bool) { return strings.Index(source, "}}") + 2, true }
	for _, source := range []string{
		"value=${{ number }}+1",
		"value=[${{ number }},2]",
		"value={${{ number }}:2}",
		"value=(${{ number }})",
		"value=${{\n number\n}}+1",
		"value='item_${{ name }}.0'",
		"value=\"item_${{ name }}.0\"",
		"# item_${{ name }}.0\nvalue=1",
	} {
		t.Run(source, func(t *testing.T) {
			got, valid, err := Sanitize(source, end)
			if err != nil || !valid || len([]rune(got)) != len([]rune(source)) {
				t.Fatalf("got %q, valid %v, error %v", got, valid, err)
			}
			for i, r := range []rune(source) {
				if (r == '\n' || r == '\r') && []rune(got)[i] != r {
					t.Fatal("line ending moved")
				}
			}
		})
	}
}

func TestFStringConversionTemplates(t *testing.T) {
	end := func(source string) (int, bool) { index := strings.Index(source, "}}"); return index + 2, index >= 0 }
	for _, source := range []string{
		`print(f"{1:'>5} {2!${{ 'r' }}}")`,
		`print(f"{1:{2!${{ 'r' }}}}")`,
		`print(f"{ {'key': 1} !${{ 'r' }}}")`,
		`print(f"{(lambda: 1)()!${{ 'r' }}}")`,
		"print(f\"\"\"{1 # ' comment\n!${{ 'r' }}}\"\"\")",
		`print(f"{value!${{ inputs.conversion }}}")`,
		`print(F'{value!${{ inputs.conversion }}}')`,
		`print(rf"{value!${{ inputs.conversion }}}")`,
		`print(t"{value!${{ inputs.conversion }}}")`,
		`print(rt"{value!${{ inputs.conversion }}}")`,
		`print(tr"{value!${{ inputs.conversion }}}")`,
		`print(f"{"value"!${{ inputs.conversion }}}")`,
		`print(f"{r"value"!${{ inputs.conversion }}}")`,
		`print(f"""{"""value"""!${{ inputs.conversion }}}""")`,
		`print(f"{f"{value!${{ inputs.conversion }}}"}")`,
		"print(f\"\"\"{value!${{\n inputs.conversion\n}}}\"\"\")",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			source := strings.ReplaceAll(source, "\n", ending)
			if got, valid, err := Sanitize(source, end); err != nil || valid || got != "" {
				t.Fatalf("conversion template accepted: %q, %v, %v", got, valid, err)
			}
		}
	}
	for _, source := range []string{
		`print("!${{ inputs.value }}")`,
		`print(f"literal!${{ inputs.value }}")`,
		`print(f"{{literal!${{ inputs.value }}}}")`,
		`print(f"{value != ${{ inputs.value }}}")`,
		`print(f"{${{ inputs.value }}!r}")`,
		`print(f"{'!${{ inputs.value }}'}")`,
		`print(f"{"!${{ inputs.value }}"}")`,
		`# f"{value!${{ inputs.conversion }}}"`,
	} {
		if _, valid, err := Sanitize(source, end); err != nil || !valid {
			t.Fatalf("value template skipped: %q, %v, %v", source, valid, err)
		}
	}
}

func TestSanitizeOperatorTemplates(t *testing.T) {
	end := func(source string) (int, bool) { index := strings.Index(source, "}}"); return index + 2, index >= 0 }
	for _, source := range []string{
		"if lhs ${{ inputs.operator }} rhs: pass",
		"value = lhs ${{ inputs.operator }} (rhs)",
		"value = left() ${{ inputs.operator }} -right",
		"value = 'left' ${{ inputs.operator }} 'right'",
		"value = values[0] ${{ inputs.operator }} other",
		"value = ${{ inputs.left }} ${{ inputs.operator }} other",
		"value = (left\n ${{ inputs.operator }}\n right)",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			source := strings.ReplaceAll(source, "\n", ending)
			t.Run(source, func(t *testing.T) {
				if got, valid, err := Sanitize(source, end); err != nil || valid || got != "" {
					t.Fatalf("operator template accepted: %q, %v, %v", got, valid, err)
				}
			})
		}
	}
	for _, source := range []string{
		"if ${{ inputs.value }}: pass", "value = left + ${{ inputs.value }}",
		"if False: pass\nelif ${{ inputs.value }}: pass",
		"try: pass\nexcept ${{ inputs.error }}: pass", "raise ${{ inputs.error }}",
		"return ${{ inputs.value }}", "print(${{ inputs.value }})",
		"value = ${{ inputs.value }} + other", "value = ${{ inputs.value }} if test else other",
		"value = [${{ inputs.value }} for item in items]",
		"if left == ${{ inputs.value }} and right: pass",
		"value = 'lhs ${{ inputs.operator }} rhs'", "# lhs ${{ inputs.operator }} rhs",
	} {
		t.Run(source, func(t *testing.T) {
			if _, valid, err := Sanitize(source, end); err != nil || !valid {
				t.Fatalf("value template skipped: %v, %v", valid, err)
			}
		})
	}
}

func TestImportFromTemplatesRemainUnsupported(t *testing.T) {
	end := func(source string) (int, bool) { index := strings.Index(source, "}}"); return index + 2, index >= 0 }
	for _, source := range []string{
		"from ${{ inputs.module }} import name",
		"from module import ${{ inputs.name }}",
		"raise ValueError()\nfrom ${{ inputs.module }} import name",
		"raise ValueError(); from ${{ inputs.module }} import name",
		"def generate():\n    yield 1\n    from ${{ inputs.module }} import name",
		"def generate():\n    yield 1; from ${{ inputs.module }} import name",
		"if True: from ${{ inputs.module }} import name",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			source := strings.ReplaceAll(source, "\n", ending)
			t.Run(source, func(t *testing.T) {
				if got, valid, err := Sanitize(source, end); err != nil || valid || got != "" {
					t.Fatalf("import template accepted: %q, %v, %v", got, valid, err)
				}
			})
		}
	}
}

func TestPercentMaskIsValidConversion(t *testing.T) {
	source, valid, err := Sanitize(`print("%03${{ inputs.kind }}" % 1)`, reviewExpressionEnd)
	if err != nil || !valid || !strings.Contains(source, "%03s") {
		t.Fatalf("mask manufactured an unsupported conversion: %q valid=%v err=%v", source, valid, err)
	}
}

func reviewExpressionEnd(source string) (int, bool) {
	end := strings.Index(source, "}}")
	return end + 2, end >= 0
}

func TestScriptSkipsUnmaskableTemplates(t *testing.T) {
	prepared, err := Script("import ${{ inputs.module }}")
	if err != nil || prepared.Skip == "" {
		t.Fatalf("name template reached the tool: %+v %v", prepared, err)
	}
	prepared, err = Script("print(1)")
	if err != nil || prepared.Skip != "" || prepared.Source != "print(1)" || prepared.TemplateContract != "" {
		t.Fatalf("plain script changed: %+v %v", prepared, err)
	}
}
