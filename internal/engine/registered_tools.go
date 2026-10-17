package engine

import (
	"context"

	findingmodel "actionlint.kjanat.dev/internal/finding"
	"actionlint.kjanat.dev/internal/tools"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/execution"
)

type registeredTool struct{ runner tools.Runner }

func (tool registeredTool) AnalyzeScripts(ctx context.Context, batch scriptBatch) ([]*findingmodel.Finding, error) {
	prepared := tools.Batch{}
	scopes := map[*scriptScope]*tools.Scope{}
	for _, scope := range batch.scopes {
		item := &tools.Scope{Config: scope.config, AnalysisDir: scope.paths.Analysis, Workspace: scope.paths.Workspace, ActionPath: scope.actionPath, Reader: scope.paths.Reader, Composite: scope.composite}
		scopes[scope] = item
		prepared.Scopes = append(prepared.Scopes, item)
	}
	for _, s := range batch.scripts {
		prepared.Scripts = append(prepared.Scripts, tools.Script{Text: s.run.Run.Value, Source: s.run.Source, Key: s.run.RunPos, Value: s.run.Run.Pos, Shell: s.shell, Config: s.config, Path: s.path, Content: s.content, Scope: scopes[s.scope], Execution: tools.Execution{Shell: s.execution.shell.Name, ImplicitShell: s.execution.shell.Implicit, Directory: s.execution.directory.Path, DirectoryKnown: s.execution.directory.Kind != execution.DirectoryUnknown}})
	}
	findings, err := tool.runner.Analyze(ctx, prepared)
	out := make([]*findingmodel.Finding, 0, len(findings))
	for _, result := range findings {
		finding := findingmodel.FromDiagnostic(result.Diagnostic)
		finding.Source, finding.RawScriptSource = result.Content, result.RawScript
		if result.Script >= 0 && result.Script < len(batch.scripts) {
			finding.IgnoreOrigin = batch.scripts[result.Script].origin
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
