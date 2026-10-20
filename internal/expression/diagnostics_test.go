package expression

import "testing"

func TestDiagnosticIdentities(t *testing.T) {
	for _, tc := range []struct {
		source, rule, code string
		script             bool
	}{
		{"github.[", "expression-syntax", "invalid-syntax", false},
		{"missing", "expression-reference", "unknown-variable", false},
		{"github.missing", "expression-reference", "unknown-property", false},
		{"missing()", "expression-reference", "unknown-function", false},
		{"secrets.TOKEN", "context-availability", "unavailable-context", false},
		{"hashFiles('x')", "context-availability", "unavailable-function", false},
		{"contains", "expression-call", "missing-call", false},
		{"contains('x')", "expression-call", "argument-count", false},
		{"fromJSON('{')", "expression-call", "invalid-json", false},
		{"contains(github, 'x')", "expression-type", "argument-type", false},
		{"github.event.issue.title", "script-injection", "untrusted-interpolation", true},
	} {
		t.Run(tc.rule+"/"+tc.code, func(t *testing.T) {
			node, parseError := NewParser().Parse(NewLexer(tc.source + "}}"))
			var findings []*Error
			if parseError != nil {
				findings = []*Error{parseError}
			} else {
				checker := NewChecker(tc.script, nil)
				checker.SetContextAvailability([]string{"github"})
				_, findings = checker.Check(node)
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
