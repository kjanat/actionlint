package actionlint

import (
	"strings"
	"testing"
)

func TestUnsoundTernary(t *testing.T) {
	for _, tc := range []struct {
		expression string
		count      int
	}{
		{"github.ref && '' || 'fallback'", 1},
		{"github.ref && false || true", 1},
		{"github.ref && 0 || 1", 1},
		{"github.ref && -0.0 || 1", 1},
		{"github.ref && null || 'fallback'", 1},
		{"github.ref && fromJSON('false') || 'fallback'", 1},
		{"github.ref && join('') || 'fallback'", 1},
		{"format('{0}', github.ref && '' || 'fallback')", 1},
		{"(github.ref && (github.sha && '' || 'ok')) || 'fallback'", 1},
		{"github.ref && 'false' || 'fallback'", 0},
		{"github.ref && '0' || 'fallback'", 0},
		{"github.ref && github.sha || 'fallback'", 0},
		{"github.ref && fromJSON('[]') || 'fallback'", 0},
		{"github.ref && (false || true)", 0},
		{"github.ref || ''", 0},
		{"github.ref && fromJSON('broken') || 'fallback'", 0},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			rule := NewRuleExpression(nil, nil)
			rule.checkExprsIn("${{ "+tc.expression+" }}", &Pos{Line: 3, Col: 5}, false, false, "env")
			count := 0
			for _, finding := range rule.Errs() {
				if finding.Kind == "unsound-ternary" {
					count++
					if finding.Line != 3 || finding.Column < 5 {
						t.Fatalf("bad location: %+v", finding)
					}
				}
			}
			if count != tc.count {
				t.Fatalf("got %d findings, want %d: %+v", count, tc.count, rule.Errs())
			}
		})
	}
}

func TestUnsoundTernaryConfigurationAndSuppression(t *testing.T) {
	src := "on: push\nenv:\n  VALUE: ${{ github.ref && '' || 'fallback' }}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
	for _, config := range []string{"", "lint: {rules: {correctness: {unsound-ternary: off}}}", "lint: {rules: {correctness: {unsound-ternary: warn}}}"} {
		cfg, err := ParseConfig([]byte(config))
		if err != nil {
			t.Fatal(err)
		}
		result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(src), Config: cfg}}})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(config, "off") {
			if len(result.Diagnostics) != 0 {
				t.Fatal(result.Diagnostics)
			}
			continue
		}
		if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "unsound-ternary" {
			t.Fatal(result.Diagnostics)
		}
		if strings.Contains(config, "warn") && result.Diagnostics[0].Severity != "warning" {
			t.Fatal(result.Diagnostics)
		}
	}
	src = strings.Replace(src, "|| 'fallback' }}", "|| 'fallback' }} # actionlint:ignore unsound-ternary -- testing fallback behavior", 1)
	result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(src)}}})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("%+v, %v", result, err)
	}
}
