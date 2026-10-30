package python_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/tools/profile/python"
)

func TestSanitizePythonScript(t *testing.T) {
	for _, source := range []string{"print(${{ github.sha }})", "print('${{ '}}' }}')", "print(${{\n github.sha\n}})", "print(${{ 'é' }}, missing)"} {
		got, valid, err := python.Sanitize(source, python.TemplateEnd)
		if err != nil {
			t.Fatal(err)
		}
		if !valid || strings.Contains(got, "${{") || len([]rune(got)) != len([]rune(source)) {
			t.Fatalf("%q -> %q, %v", source, got, valid)
		}
		for i, r := range []rune(source) {
			if (r == '\n' || r == '\r') && []rune(got)[i] != r {
				t.Fatal("line break moved")
			}
		}
	}
	if _, valid, _ := python.Sanitize("print(${{ missing)", python.TemplateEnd); valid {
		t.Fatal("accepted malformed template")
	}
}
