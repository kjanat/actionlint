package expressions

import (
	"testing"

	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	exprlang "actionlint.kjanat.dev/v2/internal/languages/expressions/typecheck"
)

func TestDiagnosticIdentities(t *testing.T) {
	for _, tc := range []struct {
		source, rule, code string
		script             bool
	}{
		{"missing", "expression-reference", "unknown-variable", false},
		{"github.missing", "expression-reference", "unknown-property", false},
		{"missing()", "expression-reference", "unknown-function", false},
		{"secrets.TOKEN", "context-availability", "unavailable-context", false},
		{"hashFiles('x')", "context-availability", "unavailable-function", false},
		{"contains", "expression-call", "missing-call", false},
		{"contains('x')", "expression-call", "argument-count", false},
		{"fromJSON('{')", "expression-call", "invalid-json", false},
		{"github.event.issue.title", "script-injection", "untrusted-interpolation", true},
	} {
		t.Run(tc.rule+"/"+tc.code, func(t *testing.T) {
			node, parseError := exprsyntax.NewParser().Parse(exprsyntax.NewLexer(tc.source + "}}"))
			var findings []*exprlang.Error
			if parseError != nil {
				t.Fatalf("invalid diagnostic fixture: %v", parseError)
			} else {
				checker := exprlang.NewChecker(nil)
				checker.SetContextAvailability([]string{"github"})
				_, findings = checker.Check(node)
				if tc.script {
					findings = append(findings, checkUntrustedInputs(node)...)
				}
			}
			if len(findings) == 0 {
				t.Fatal("expected diagnostic")
			}
			for _, finding := range findings {
				if finding.Rule != tc.rule || finding.Code != tc.code {
					t.Fatalf("got %s/%s: %s; want %s/%s", finding.Rule, finding.Code, finding.Message, tc.rule, tc.code)
				}
			}
		})
	}
}
