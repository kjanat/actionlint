package execution

import (
	"context"

	findingmodel "actionlint.kjanat.dev/internal/finding"
	"actionlint.kjanat.dev/internal/tools"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	workflowexecution "actionlint.kjanat.dev/internal/workflow/execution"
)

type Tool struct{ Runner tools.Runner }

func (tool Tool) AnalyzeScripts(ctx context.Context, batch Batch) ([]*findingmodel.Finding, error) {
	prepared := tools.Batch{}
	scopes := map[*Scope]*tools.Scope{}
	for _, scope := range batch.Scopes {
		item := &tools.Scope{Config: scope.Config, AnalysisDir: scope.Paths.Analysis, Workspace: scope.Paths.Workspace, ActionPath: scope.ActionPath, Reader: scope.Paths.Reader, Composite: scope.Composite}
		scopes[scope] = item
		prepared.Scopes = append(prepared.Scopes, item)
	}
	for _, s := range batch.Scripts {
		prepared.Scripts = append(prepared.Scripts, tools.Script{Text: s.Run.Run.Value, Source: s.Run.Source, Key: s.Run.RunPos, Value: s.Run.Run.Pos, Shell: s.Shell, Config: s.Config, Path: s.Path, Content: s.Content, Scope: scopes[s.Scope], Execution: tools.Execution{Shell: s.Execution.Shell.Name, ImplicitShell: s.Execution.Shell.Implicit, Directory: s.Execution.Directory.Path, DirectoryKnown: s.Execution.Directory.Kind != workflowexecution.DirectoryUnknown}})
	}
	findings, err := tool.Runner.Analyze(ctx, prepared)
	out := make([]*findingmodel.Finding, 0, len(findings))
	for _, result := range findings {
		finding := findingmodel.FromDiagnostic(result.Diagnostic)
		finding.Source, finding.RawScriptSource = result.Content, result.RawScript
		if result.Script >= 0 && result.Script < len(batch.Scripts) {
			finding.IgnoreOrigin = batch.Scripts[result.Script].Origin
			finding.Owner = batch.Scripts[result.Script].Owner
		}
		if at := result.Suppression; at != nil {
			finding.SuppressionOrigin = &findingmodel.SuppressionOrigin{Path: at.Path, First: at.First, Last: at.Last}
		}
		if result.End.Line > 0 {
			finding.EndPosition = &workflowlang.Pos{Line: result.End.Line, Col: result.End.Column}
			if result.End.Line == result.Start.Line {
				finding.EndColumn = result.End.Column - 1
			}
		}
		out = append(out, finding)
	}
	return out, err
}
