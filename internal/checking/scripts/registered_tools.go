package scripts

import (
	"context"
	"fmt"

	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	workflowexecution "actionlint.kjanat.dev/v2/internal/model/workflows/execution"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

type Tool struct {
	Name   string
	Runner tools.Runner
}

func (tool Tool) AnalyzeScripts(ctx context.Context, batch Batch) ([]*findingmodel.Finding, error) {
	if !batch.Valid() {
		return nil, ErrInvalidBatch
	}
	batchScopes, batchScripts := batch.Snapshot()
	prepared := tools.Batch{CallGraph: batch.Invocations()}
	scopes := map[*Scope]*tools.Scope{}
	for _, scope := range batchScopes {
		config := scope.Config
		if scope.Bindings != nil {
			config = scope.Bindings.Rule(tool.Name).Config()
		}
		config, err := config.Detached()
		if err != nil {
			return nil, fmt.Errorf("prepare tool %q configuration: %w", tool.Name, err)
		}
		item := &tools.Scope{Config: config, ConfigProvenance: scope.ConfigProvenance, AnalysisDir: scope.Paths.Analysis, Workspace: scope.Paths.Workspace, ActionPath: scope.Deployment.ActionPath(), Reader: scope.Reader, Composite: scope.Composite}
		scopes[scope] = item
		prepared.Scopes = append(prepared.Scopes, item)
	}
	for index, s := range batchScripts {
		script := tools.Script{Shell: s.Shell, Config: scopes[s.Scope].Config, Path: s.Path, Content: s.Content, Scope: scopes[s.Scope], Execution: tools.Execution{Shell: s.Execution.Shell.Name, ImplicitShell: s.Execution.Shell.Implicit, Directory: s.Execution.Directory.Path, DirectoryKnown: s.Execution.Directory.Kind != workflowexecution.DirectoryUnknown}}
		fact, exists := batch.Fact(index, true)
		if !exists {
			return nil, ErrInvalidBatch
		}
		script.Fact = fact
		if input := s.Input; input != nil {
			site := input.Site
			script.Input, script.Text, script.Source = &site, input.Value.Value.Text(), input.Value.Source
			script.Key, script.Value = input.Value.Name.Pos, input.Value.Value.Pos
		} else {
			script.Text, script.Source = s.Run.Run.Text(), s.Run.Source
			script.Key, script.Value = s.Run.RunPos, s.Run.Run.Pos
		}
		prepared.Scripts = append(prepared.Scripts, script)
	}
	findings, err := tool.Runner.Analyze(ctx, prepared)
	out := make([]*findingmodel.Finding, 0, len(findings))
	for _, result := range findings {
		finding := findingmodel.FromDiagnostic(result.Diagnostic)
		finding.Source, finding.RawScriptSource = result.Content, result.RawScript
		finding.UnavailableSource = result.UnavailableSource
		if result.Script >= 0 && result.Script < len(batchScripts) {
			if scope := batchScripts[result.Script].Scope.Bindings; scope != nil {
				finding.Binding = scope.Rule(finding.Rule).ID()
			}
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
