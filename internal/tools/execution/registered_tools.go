package execution

import (
	"context"

	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/tools"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowexecution "actionlint.kjanat.dev/v2/internal/workflow/execution"
)

type Tool struct{ Runner tools.Runner }

func (tool Tool) AnalyzeScripts(ctx context.Context, batch Batch) ([]*findingmodel.Finding, error) {
	if !batch.Valid() {
		return nil, ErrInvalidBatch
	}
	batchScopes, batchScripts := batch.Snapshot()
	prepared := tools.Batch{CallGraph: batch.CallGraph()}
	scopes := map[*Scope]*tools.Scope{}
	for _, scope := range batchScopes {
		item := &tools.Scope{Config: scope.Config, AnalysisDir: scope.Paths.Analysis, Workspace: scope.Paths.Workspace, ActionPath: scope.Deployment.ActionPath(), Reader: scope.Paths.Reader, Composite: scope.Composite}
		scopes[scope] = item
		prepared.Scopes = append(prepared.Scopes, item)
	}
	for index, s := range batchScripts {
		script := tools.Script{Shell: s.Shell, Config: s.Config, Path: s.Path, Content: s.Content, Scope: scopes[s.Scope], Execution: tools.Execution{Shell: s.Execution.Shell.Name, ImplicitShell: s.Execution.Shell.Implicit, Directory: s.Execution.Directory.Path, DirectoryKnown: s.Execution.Directory.Kind != workflowexecution.DirectoryUnknown}}
		fact, exists := batch.Fact(index, true)
		if !exists {
			return nil, ErrInvalidBatch
		}
		script.Fact = fact
		if input := s.Input; input != nil {
			site := input.Site
			script.Input, script.Text, script.Source = &site, input.Value.Value.Value, input.Value.Source
			script.Key, script.Value = input.Value.Name.Pos, input.Value.Value.Pos
		} else {
			script.Text, script.Source = s.Run.Run.Value, s.Run.Source
			script.Key, script.Value = s.Run.RunPos, s.Run.Run.Pos
		}
		prepared.Scripts = append(prepared.Scripts, script)
	}
	findings, err := tool.Runner.Analyze(ctx, prepared)
	out := make([]*findingmodel.Finding, 0, len(findings))
	for _, result := range findings {
		finding := findingmodel.FromDiagnostic(result.Diagnostic)
		finding.Source, finding.RawScriptSource = result.Content, result.RawScript
		if result.Script >= 0 && result.Script < len(batchScripts) {
			finding.IgnoreOrigin = batchScripts[result.Script].Origin
			finding.Owner = batchScripts[result.Script].Owner
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
