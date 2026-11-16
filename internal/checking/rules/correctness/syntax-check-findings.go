package correctness

import (
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

// SyntaxCheckDiagnostic supplies a parser issue's source excerpt and diagnostic range.
func SyntaxCheckDiagnostic(issue *parser.SyntaxError, source []byte) results.Diagnostic {
	snippet, _ := sourcemap.SourceLine(string(source), issue.Position.Line)
	return results.Diagnostic{
		Rule: issue.Kind, Code: string(issue.Code), Message: issue.Message,
		Start:   issue.Position,
		End:     sourcepos.Position{Line: issue.Position.Line, Column: sourcepos.TokenEndColumn(snippet, issue.Position.Column)},
		Snippet: snippet,
	}
}

// SyntaxCheckFindings converts parser issues to findings for rule execution.
func SyntaxCheckFindings(issues []*parser.SyntaxError) []*finding.Finding {
	findings := make([]*finding.Finding, 0, len(issues))
	for _, issue := range issues {
		findings = append(findings, &finding.Finding{Message: issue.Message, Start: issue.Position, Rule: issue.Kind})
	}
	return findings
}
