package actionlint

import (
	"strings"
	"testing"
)

func TestConstantFormatNumericArguments(t *testing.T) {
	for _, tc := range []struct{ expression, want string }{
		{"format('{0}', 0)", "0"},
		{"format('{0}', 0.0)", "0"},
		{"format('{0}', 123)", "123"},
		{"format('{0}', -123)", "-123"},
		{"format('{0}', 123.0)", "123"},
		{"format('{0}', 1e3)", "1000"},
		{"format('{0}', 999999999999999.0)", "999999999999999"},
		{"format('{0}', -999999999999999.0)", "-999999999999999"},
		{"format('{0}', fromJSON('42'))", "42"},
		{"format('{{{1}}}:{0}:{1}', 0, -12)", "{-12}:0:-12"},
		{"format('{0}:{1}:{2}', 'text', false, null)", "text:false:"},
		{"format('{0}', null)", ""},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			expr := parseAssignedExpression("${{ " + tc.expression + " }}")
			if expr == nil {
				t.Fatal("invalid expression fixture")
			}
			value, known := conditionConstantValue(expr)
			if !known || value != tc.want {
				t.Fatalf("value=%v known=%v, want %q", value, known, tc.want)
			}
			for _, condition := range []string{tc.expression, "${{ " + tc.expression + " }}"} {
				if enabled, known := invocationCondition(&String{Value: condition}); !known || enabled != (tc.want != "") {
					t.Fatalf("invocation=%v known=%v, want %v", enabled, known, tc.want != "")
				}
			}
			want := "always falsy"
			if tc.want != "" {
				want = "always truthy"
			}
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n        if: ${{ " + tc.expression + " }}\n"
			findings := lintCachePolicy(t, source, "")
			if len(findings) != 1 || !strings.Contains(findings[0].Message, want) {
				t.Fatalf("want %q, got %v", want, findings)
			}
		})
	}
}

func TestConstantFormatUncertainArguments(t *testing.T) {
	for _, expression := range []string{
		"format('{0}', -0)", "format('{0}', -0.0)",
		"format('{0}', fromJSON('-0'))", "format('{0}', 1e15)",
		"format('{0}', -1e15)", "format('{0}', 1e-5)",
		"format('{0}', 0.12345678901234567)", "format('{0}', fromJSON('1e309'))",
		"format('{0}', fromJSON('{}'))", "format('{0}', fromJSON('[]'))",
		"format('{0}', inputs.number)",
	} {
		t.Run(expression, func(t *testing.T) {
			value, known := conditionConstantValue(parseAssignedExpression("${{ " + expression + " }}"))
			if known {
				t.Fatalf("unexpected constant value %v", value)
			}
			if _, known := invocationCondition(&String{Value: "${{ " + expression + " }}"}); known {
				t.Fatal("uncertain format argument made invocation known")
			}
		})
	}
}

func TestExecutableBitNumericFormatCheckout(t *testing.T) {
	root, _ := executableFixture(t)
	for _, tc := range []struct {
		argument string
		want     int
	}{
		{"0", 1}, {"42", 1}, {"-42", 1},
		{"-0", 0}, {"1e15", 0}, {"0.12345678901234567", 0},
	} {
		t.Run(tc.argument, func(t *testing.T) {
			workflow := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v6\n        if: ${{ format('{0}', " + tc.argument + ") }}\n      - run: ./bad.sh\n"
			file := writeShellcheckFixture(t, root, ".github/workflows/format.yml", workflow)
			session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Files([]string{file}, nil)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule == "executable-bit" {
					count++
				}
			}
			if count != tc.want {
				t.Fatalf("got %d executable-bit findings, want %d: %+v", count, tc.want, result.Diagnostics)
			}
		})
	}
}
