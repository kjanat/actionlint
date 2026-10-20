package composition

import (
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
)

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
	unit := scriptexecution.Unit{Run: run, Shell: shell, Config: cfg, Path: path, Content: content, Origin: findingmodel.StepOrigin(analysis.jobID, step), Owner: &findingmodel.Owner{Job: analysis.jobID, Step: step.Pos}, Scope: analysis.scriptScope}
	if unit.Scope != nil && analysis.resolveScriptExecution {
		paths := unit.Scope.Paths
		if analysis.actions != nil {
			paths = analysis.actions.ApplyCheckout(paths)
			paths.Platform = analysis.actions.Platform()
		}
		jobShell, workflowShell, runnerShell := analysis.scriptJobShell, analysis.workflowShell, analysis.scriptRunnerShell
		jobDir, workflowDir := analysis.scriptJobDir, analysis.scriptWorkflowDir
		if unit.Scope.Composite {
			jobShell, workflowShell, runnerShell = execution.ShellValue{}, execution.ShellValue{}, execution.ShellValue{}
			jobDir, workflowDir = execution.Directory{}, execution.Directory{}
		}
		unit.Execution = scriptexecution.Execution{
			Shell:     execution.ResolveShell(run, jobShell, workflowShell, runnerShell),
			Directory: paths.Resolve(paths.EffectiveDirectory(run, jobDir, workflowDir)),
		}
	}
	analysis.scripts = append(analysis.scripts, unit)
}
