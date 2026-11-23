package correctness

import (
	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/finding"
)

func SyntaxCheckDiagnostic(issue *parser.SyntaxError, source []byte) results.Diagnostic {
	snippet, _ := sourcemap.SourceLine(string(source), issue.Position.Line)
	return results.Diagnostic{
		Rule: issue.Kind, Code: string(issue.Code), Message: issue.Message,
		Start:   issue.Position,
		End:     sourcetext.Position{Line: issue.Position.Line, Column: sourcetext.TokenEndColumn(snippet, issue.Position.Column)},
		Snippet: snippet,
	}
}

func SyntaxCheckFindings(issues []*parser.SyntaxError) []*finding.Finding {
	findings := make([]*finding.Finding, 0, len(issues))
	for _, issue := range issues {
		findings = append(findings, &finding.Finding{Message: issue.Message, Start: issue.Position, Rule: issue.Kind})
	}
	return findings
}
