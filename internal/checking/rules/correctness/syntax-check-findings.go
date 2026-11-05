package correctness

import (
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/input/source/text"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/sourcemap"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
	diagmodel "actionlint.kjanat.dev/v2/internal/results"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

// SyntaxCheckDiagnostic supplies a parser issue's source excerpt and diagnostic range.
func SyntaxCheckDiagnostic(issue *workflowparser.SyntaxError, source []byte) diagmodel.Diagnostic {
	snippet, _ := sourcemap.SourceLine(string(source), issue.Line)
	return diagmodel.Diagnostic{
		Rule: issue.Kind, Code: string(issue.Code), Message: issue.Message,
		Start:   sourcepos.Position{Line: issue.Line, Column: issue.Column},
		End:     sourcepos.Position{Line: issue.Line, Column: text.TokenEndColumn(snippet, issue.Column)},
		Snippet: snippet,
	}
}

// SyntaxCheckFindings converts parser issues to findings for rule execution.
func SyntaxCheckFindings(issues []*workflowparser.SyntaxError) []*findingmodel.Finding {
	findings := make([]*findingmodel.Finding, 0, len(issues))
	for _, issue := range issues {
		findings = append(findings, &findingmodel.Finding{Message: issue.Message, Start: sourcepos.Position{Line: issue.Line, Column: issue.Column}, Rule: issue.Kind})
	}
	return findings
}
