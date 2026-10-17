package engine

import (
	"context"

	repoconfig "actionlint.kjanat.dev/internal/config"

	findingmodel "actionlint.kjanat.dev/internal/finding"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/execution"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
)

// scriptUnit snapshots invocation context before analyzers run. Source mapping
// remains an adapter concern; backends receive text and explicit shell selection.
type scriptUnit struct {
	run       *workflowlang.ExecRun
	shell     *string
	config    *repoconfig.Config
	path      string
	content   []byte
	origin    *findingmodel.IgnoreOrigin
	scope     *scriptScope
	execution scriptExecution
}

// scriptScope identifies a workflow or composite invocation independently of
// whether it contains scripts, so configuration failures are still observable.
type scriptScope struct {
	paths      execution.Paths
	config     *repoconfig.Config
	actionPath string
	composite  bool
	document   string
	caller     string
	call       *workflowlang.Step
	depth      int
}

type scriptExecution struct {
	shell     execution.Shell
	directory execution.Directory
}

type scriptBatch struct {
	scopes  []*scriptScope
	scripts []scriptUnit
}

type scriptAnalyzer interface {
	AnalyzeScripts(context.Context, scriptBatch) ([]*findingmodel.Finding, error)
}

type builtinAnalyzers struct {
	resolveExecution bool
	scriptInfo       []RuleInfo
	rules            []workflowvisit.Rule
	scripts          []scriptAnalyzer
}

func scriptDefaultShell(defaults *workflowlang.Defaults) *string {
	if defaults == nil || defaults.Run == nil || defaults.Run.Shell == nil {
		return nil
	}
	return &defaults.Run.Shell.Value
}

func (analysis *compositeAnalyzer) collectScript(step *workflowlang.Step, cfg *repoconfig.Config, path string, content []byte, jobShell, workflowShell *string) {
	if !analysis.collectScripts {
		return
	}
	run, ok := step.Exec.(*workflowlang.ExecRun)
	if !ok || run.Run == nil {
		return
	}
	shell := jobShell
	if shell == nil {
		shell = workflowShell
	}
	if run.Shell != nil {
		shell = &run.Shell.Value
	}
	unit := scriptUnit{run: run, shell: shell, config: cfg, path: path, content: content, origin: findingmodel.StepOrigin(analysis.jobID, step), scope: analysis.scriptScope}
	if unit.scope != nil && analysis.resolveScriptExecution {
		paths := unit.scope.paths
		if analysis.actions != nil {
			paths = analysis.actions.ApplyCheckout(paths)
			paths.Platform = analysis.actions.Platform()
		}
		jobShell, workflowShell, runnerShell := analysis.scriptJobShell, analysis.workflowShell, analysis.scriptRunnerShell
		jobDir, workflowDir := analysis.scriptJobDir, analysis.scriptWorkflowDir
		if unit.scope.composite {
			jobShell, workflowShell, runnerShell = execution.ShellValue{}, execution.ShellValue{}, execution.ShellValue{}
			jobDir, workflowDir = execution.Directory{}, execution.Directory{}
		}
		unit.execution = scriptExecution{
			shell:     execution.ResolveShell(run, jobShell, workflowShell, runnerShell),
			directory: paths.Resolve(paths.EffectiveDirectory(run, jobDir, workflowDir)),
		}
	}
	analysis.scripts = append(analysis.scripts, unit)
}
