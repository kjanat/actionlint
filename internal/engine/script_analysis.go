package engine

import "context"

// scriptUnit snapshots invocation context before analyzers run. Source mapping
// remains an adapter concern; backends receive text and explicit shell selection.
type scriptUnit struct {
	run       *ExecRun
	shell     *string
	config    *Config
	path      string
	content   []byte
	origin    *diagnosticIgnoreOrigin
	scope     *scriptScope
	execution scriptExecution
}

// scriptScope identifies a workflow or composite invocation independently of
// whether it contains scripts, so configuration failures are still observable.
type scriptScope struct {
	paths      runPaths
	config     *Config
	actionPath string
	composite  bool
}

type scriptExecution struct {
	shell     shellcheckShell
	directory runDirectory
}

type scriptBatch struct {
	scopes  []*scriptScope
	scripts []scriptUnit
}

type scriptAnalyzer interface {
	AnalyzeScripts(context.Context, scriptBatch) ([]*Error, error)
}

type builtinAnalyzers struct {
	resolveExecution bool
	scriptInfo       []RuleInfo
	rules            []Rule
	scripts          []scriptAnalyzer
}

func scriptDefaultShell(defaults *Defaults) *string {
	if defaults == nil || defaults.Run == nil || defaults.Run.Shell == nil {
		return nil
	}
	return &defaults.Run.Shell.Value
}

func (analysis *compositeAnalyzer) collectScript(step *Step, cfg *Config, path string, content []byte, jobShell, workflowShell *string) {
	if !analysis.collectScripts {
		return
	}
	run, ok := step.Exec.(*ExecRun)
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
	unit := scriptUnit{run: run, shell: shell, config: cfg, path: path, content: content, origin: newDiagnosticIgnoreOrigin(analysis.jobID, step), scope: analysis.scriptScope}
	if unit.scope != nil && analysis.resolveScriptExecution {
		paths := unit.scope.paths
		if analysis.actions != nil {
			checkout := analysis.actions.currentCheckout()
			paths.checkout, paths.checkoutUnknown = checkout.path, checkout.kind == directoryUnknown
			paths.placements, paths.caseInsensitive = analysis.actions.checkoutState(), analysis.actions.caseInsensitive
			paths.platform = analysis.actions.platform
		}
		jobShell, workflowShell, runnerShell := analysis.scriptJobShell, analysis.workflowShell, analysis.scriptRunnerShell
		jobDir, workflowDir := analysis.scriptJobDir, analysis.scriptWorkflowDir
		if unit.scope.composite {
			jobShell, workflowShell, runnerShell = shellValue{}, shellValue{}, shellValue{}
			jobDir, workflowDir = runDirectory{}, runDirectory{}
		}
		unit.execution = scriptExecution{
			shell:     resolveRunShell(run, jobShell, workflowShell, runnerShell),
			directory: paths.resolve(paths.effectiveRunDirectory(run, jobDir, workflowDir)),
		}
	}
	analysis.scripts = append(analysis.scripts, unit)
}
