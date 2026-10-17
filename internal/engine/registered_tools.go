package engine

import (
	"context"

	"actionlint.kjanat.dev/internal/tools"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

type registeredTool struct{ runner tools.Runner }

func (tool registeredTool) AnalyzeScripts(ctx context.Context, batch scriptBatch) ([]*Error, error) {
	prepared := tools.Batch{}
	scopes := map[*scriptScope]*tools.Scope{}
	for _, scope := range batch.scopes {
		item := &tools.Scope{Config: scope.config, AnalysisDir: scope.paths.analysis, Workspace: scope.paths.workspace, ActionPath: scope.actionPath, Reader: scope.paths.reader, Composite: scope.composite}
		scopes[scope] = item
		prepared.Scopes = append(prepared.Scopes, item)
	}
	for _, s := range batch.scripts {
		prepared.Scripts = append(prepared.Scripts, tools.Script{Text: s.run.Run.Value, Source: s.run.Source, Key: s.run.RunPos, Value: s.run.Run.Pos, Shell: s.shell, Config: s.config, Path: s.path, Content: s.content, Scope: scopes[s.scope], Execution: tools.Execution{Shell: s.execution.shell.name, ImplicitShell: s.execution.shell.implicit, Directory: s.execution.directory.path, DirectoryKnown: s.execution.directory.kind != directoryUnknown}})
	}
	findings, err := tool.runner.Analyze(ctx, prepared)
	out := make([]*Error, 0, len(findings))
	for _, result := range findings {
		finding := findingOf(result.Diagnostic)
		finding.source, finding.rawScriptSource = result.Content, result.RawScript
		if result.Script >= 0 && result.Script < len(batch.scripts) {
			finding.ignoreOrigin = batch.scripts[result.Script].origin
		}
		if at := result.Suppression; at != nil {
			finding.suppressionOrigin = &suppressionOrigin{path: at.Path, first: at.First, last: at.Last}
		}
		if result.End.Line > 0 {
			finding.endPosition = &workflowlang.Pos{Line: result.End.Line, Col: result.End.Column}
			if result.End.Line == result.Start.Line {
				finding.endColumn = result.End.Column - 1
			}
		}
		out = append(out, finding)
	}
	return out, err
}
